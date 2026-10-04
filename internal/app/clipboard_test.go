package app

import (
	"bytes"
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"github.com/weshofmann/boxwarden/internal/config"
	"io"
	"testing"
)

type clipboardFake struct {
	request clipboardx.Request
	calls   int
}

func (f *clipboardFake) Execute(_ context.Context, name string, r clipboardx.Request, _ io.Reader, out io.Writer, _ clipboardx.Pasteboard) (clipboardx.Outcome, error) {
	f.calls++
	f.request = r
	if name != "dev" {
		panic("wrong name")
	}
	if r.Mode == clipboardx.Paste {
		out.Write([]byte(" unicode ☃\n\n"))
	}
	return clipboardx.Committed, nil
}
func TestClipboardRoutesExactOutputAndBinding(t *testing.T) {
	path, selected := writeDomainFixture(t, "work")
	for _, mode := range []string{"push", "pull", "copy", "paste"} {
		var out bytes.Buffer
		f := &clipboardFake{}
		opts := Options{Output: &out, Input: bytes.NewBufferString("x"), ClipboardTransferFactory: func(_ context.Context, _ config.Config, d config.Domain) (ClipboardTransfer, error) {
			if d != selected {
				t.Fatal("wrong domain")
			}
			return f, nil
		}}
		err := Run(t.Context(), []string{"--config", path, "--domain", "work", "clipboard", mode, "dev"}, opts)
		if err != nil || f.calls != 1 || string(f.request.Mode) != mode {
			t.Fatalf("%s: %v", mode, err)
		}
		if mode == "paste" && out.String() != " unicode ☃\n\n" {
			t.Fatal("stdout changed")
		}
		if mode != "paste" && out.Len() != 0 {
			t.Fatal("diagnostic on stdout")
		}
	}
}
func TestClipboardParserRejectsIncompleteBindingAndRawModes(t *testing.T) {
	for _, args := range [][]string{{"clipboard", "push", "--raw", "dev"}, {"clipboard", "paste", "--expected-generation", "g", "dev"}, {"clipboard", "copy", "bad-name"}, {"clipboard", "paste", "--raw", "dev", "extra"}} {
		if _, err := parseCommand(append([]string{"--domain", "work"}, args...), Options{}); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestClipboardPasteTerminalRefusedBeforeFactory(t *testing.T) {
	path, _ := writeDomainFixture(t, "work")
	calls := 0
	err := Run(t.Context(), []string{"--config", path, "--domain", "work", "clipboard", "paste", "dev"}, Options{Output: &bytes.Buffer{}, OutputTerminal: true, ClipboardTransferFactory: func(context.Context, config.Config, config.Domain) (ClipboardTransfer, error) {
		calls++
		return &clipboardFake{}, nil
	}})
	if err != clipboardx.ErrTerminal || calls != 0 {
		t.Fatalf("error=%v calls=%d", err, calls)
	}
}

func TestClipboardParserAcceptsExactTartArgumentOrder(t *testing.T) {
	target := clipboardx.Target{Domain: "personal", SessionID: "11111111-1111-4111-8111-111111111111", BackendKind: "tart", BackendObject: "bw-alpha", Generation: "22222222-2222-4222-8222-222222222222"}
	for _, mode := range []string{"push", "pull"} {
		args := []string{"--config", "/opt/config", "--domain", "personal", "clipboard", mode, "alpha", "--expected-session-id", target.SessionID, "--expected-backend-kind", target.BackendKind, "--expected-backend-object", target.BackendObject, "--expected-generation", target.Generation}
		command, err := parseCommand(args, Options{})
		if err != nil || command.kind != commandClipboard || command.name != "alpha" || command.clipboard.Target != target || command.clipboard.Mode != clipboardx.Mode(mode) {
			t.Fatalf("Tart %s invocation: %+v, %v", mode, command, err)
		}
	}
}
func TestClipboardParserAcceptsRawAfterSessionAndPreservesValueBytes(t *testing.T) {
	for _, args := range [][]string{{"clipboard", "paste", "dev", "--raw"}, {"clipboard", "paste", "--raw", "dev"}, {"clipboard", "paste", "dev", "--raw=true"}} {
		command, err := parseCommand(append([]string{"--domain", "work"}, args...), Options{})
		if err != nil || !command.clipboard.Raw || command.name != "dev" {
			t.Fatalf("raw args %v: %+v, %v", args, command, err)
		}
	}
	backend := "synthetic ☃;$(literal)\n"
	command, err := parseCommand([]string{"--domain", "work", "clipboard", "push", "dev", "--expected-session-id=one", "--expected-backend-kind", "tart", "--expected-backend-object", backend, "--expected-generation", "two"}, Options{})
	if err != nil || command.clipboard.Target.BackendObject != backend {
		t.Fatalf("argument value bytes changed: %v", err)
	}
}
func TestClipboardParserRejectsMalformedInterspersedFlags(t *testing.T) {
	for _, args := range [][]string{{"paste", "dev", "--expected-generation", "g"}, {"paste", "dev", "--unknown"}, {"paste", "dev", "--expected-generation"}, {"paste", "dev", "--raw", "extra"}, {"copy", "dev", "--raw"}, {"paste", "dev", "--", "--raw"}} {
		if _, err := parseCommand(append([]string{"--domain", "work", "clipboard"}, args...), Options{}); err == nil {
			t.Fatalf("accepted malformed args %v", args)
		}
	}
}

func TestClipboardViewerExplicitConfigWorksWithoutHome(t *testing.T) {
	path, selected := writeDomainFixture(t, "work")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	fake := &clipboardFake{}
	options := Options{Env: []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8"}, Output: &bytes.Buffer{}, ClipboardTransferFactory: func(_ context.Context, _ config.Config, d config.Domain) (ClipboardTransfer, error) {
		if d != selected {
			t.Fatal("wrong domain")
		}
		return fake, nil
	}}
	args := []string{"--config", path, "--domain", "work", "clipboard", "push", "dev", "--expected-session-id", "11111111-1111-4111-8111-111111111111", "--expected-backend-kind", "tart", "--expected-backend-object", "bw-dev", "--expected-generation", "22222222-2222-4222-8222-222222222222"}
	if err := Run(t.Context(), args, options); err != nil || fake.calls != 1 {
		t.Fatalf("closed environment explicit locator failed: %v calls=%d", err, fake.calls)
	}
}

func TestClipboardPrivatePasteboardFlagIsExplicitAndPushPullOnly(t *testing.T) {
	for _, mode := range []string{"push", "pull"} {
		for _, args := range [][]string{{"clipboard", mode, "dev", "--private-host-pasteboard", "org.boxwarden.test.native-123"}, {"clipboard", mode, "--private-host-pasteboard=org.boxwarden.test.native-123", "dev"}} {
			if _, err := parseCommand(append([]string{"--domain", "work"}, args...), Options{}); err != nil {
				t.Fatalf("private synthetic board rejected %v: %v", args, err)
			}
		}
	}
	for _, name := range []string{"", "general", "org.boxwarden.test.", "org.boxwarden.test.fake\n", "org.boxwarden.test.fake\x00", "org.boxwarden.production.fake"} {
		if _, err := parseCommand([]string{"--domain", "work", "clipboard", "push", "--private-host-pasteboard", name, "dev"}, Options{}); err == nil {
			t.Fatalf("unsafe private name accepted: %q", name)
		}
	}
	for _, mode := range []string{"copy", "paste"} {
		if _, err := parseCommand([]string{"--domain", "work", "clipboard", mode, "--private-host-pasteboard", "org.boxwarden.test.native-123", "dev"}, Options{}); err == nil {
			t.Fatalf("board accepted on %s", mode)
		}
	}
}

type clipboardBoardWitness struct{ label string }

func (*clipboardBoardWitness) ReadText(context.Context) ([]byte, error) {
	panic("unexpected pasteboard read")
}
func (*clipboardBoardWitness) WriteText(context.Context, []byte) (clipboardx.Outcome, error) {
	panic("unexpected pasteboard write")
}

type clipboardBoardRecorder struct {
	board clipboardx.Pasteboard
	calls int
}

func (f *clipboardBoardRecorder) Execute(_ context.Context, _ string, _ clipboardx.Request, _ io.Reader, _ io.Writer, board clipboardx.Pasteboard) (clipboardx.Outcome, error) {
	f.board = board
	f.calls++
	return clipboardx.Committed, nil
}

func TestClipboardPrivatePasteboardRoutesWithoutGeneralFallback(t *testing.T) {
	path, _ := writeDomainFixture(t, "work")
	for _, mode := range []string{"push", "pull"} {
		t.Run(mode, func(t *testing.T) {
			privateBoard, generalBoard := &clipboardBoardWitness{"private"}, &clipboardBoardWitness{"general"}
			out := &bytes.Buffer{}
			transfer := &clipboardBoardRecorder{}
			opts := Options{Output: out, Pasteboard: generalBoard, ClipboardTransferFactory: func(context.Context, config.Config, config.Domain) (ClipboardTransfer, error) { return transfer, nil }, PrivatePasteboardFactory: func(name string) (clipboardx.Pasteboard, error) {
				if name != "org.boxwarden.test.synthetic" {
					t.Fatal("board retargeted")
				}
				return privateBoard, nil
			}}
			args := []string{"--config", path, "--domain", "work", "clipboard", mode, "--private-host-pasteboard", "org.boxwarden.test.synthetic", "dev"}
			if err := Run(t.Context(), args, opts); err != nil || transfer.calls != 1 || transfer.board != privateBoard || out.Len() != 0 {
				t.Fatalf("private route failed: %v %#v", err, transfer)
			}
			transfer.calls = 0
			opts.PrivatePasteboardFactory = nil
			if err := Run(t.Context(), args, opts); err != clipboardx.ErrRequest || transfer.calls != 0 {
				t.Fatalf("missing factory fell back: %v", err)
			}
			opts.PrivatePasteboardFactory = func(string) (clipboardx.Pasteboard, error) { return nil, clipboardx.ErrUnavailable }
			if err := Run(t.Context(), args, opts); err != clipboardx.ErrRequest || transfer.calls != 0 {
				t.Fatalf("failed board fell back: %v", err)
			}
			opts.Env = []string{"BOXWARDEN_PRIVATE_PASTEBOARD=org.boxwarden.test.ambient"}
			opts.PrivatePasteboardFactory = func(string) (clipboardx.Pasteboard, error) {
				t.Fatal("ambient environment selected private board")
				return nil, nil
			}
			if err := Run(t.Context(), []string{"--config", path, "--domain", "work", "clipboard", mode, "dev"}, opts); err != nil || transfer.calls != 1 || transfer.board != generalBoard {
				t.Fatalf("default route changed: %v", err)
			}
		})
	}
}
