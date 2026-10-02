//go:build darwin && cgo

package clipboardhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func privateBoardForTest(name string) clipboardx.Pasteboard {
	b := newBoard(name).(*processBoard)
	b.command = privateHelperCommand
	return b
}
func privateHelperCommand(ctx context.Context, mode, name string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrivatePasteboardHelperProcess$", "--", "boxwarden-private-clipboard-helper", mode, name), nil
}

type lostAckWriter struct{ output io.Writer }

func (w lostAckWriter) Write(data []byte) (int, error) {
	if len(data) == 1 && data[0] == helperCommitted {
		return 0, io.ErrClosedPipe
	}
	return w.output.Write(data)
}
func TestPrivatePasteboardHelperProcess(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "boxwarden-private-clipboard-helper" {
		return
	}
	mode, name := os.Args[len(os.Args)-2], os.Args[len(os.Args)-1]
	if !validBoardName(name) || name == "" {
		os.Exit(90)
	}
	if os.Getenv("BOXWARDEN_PRIVATE_TEST_AMBIENT") != "" || os.Getenv("HOME") != "" {
		os.Exit(91)
	}
	if mode == "hang" {
		for {
			time.Sleep(time.Second)
		}
	}
	if mode == "oversized" || mode == "invalid" || mode == "trailing" {
		// Match the real read handshake before emitting malformed output. Otherwise
		// this child can exit before the parent writes, racing frame errors with EPIPE.
		token, err := readHelperByte(os.Stdin)
		if err != nil || token != helperStart || !helperEOF(os.Stdin) {
			os.Exit(93)
		}
		os.Stdout.Write([]byte{helperReady})
		switch mode {
		case "oversized":
			binary.Write(os.Stdout, binary.BigEndian, uint32(clipboardx.MaxTextBytes+1))
		case "invalid":
			binary.Write(os.Stdout, binary.BigEndian, uint32(1))
			os.Stdout.Write([]byte{255})
		case "trailing":
			clipboardx.WriteTextFrame(os.Stdout, []byte("synthetic"))
			os.Stdout.Write([]byte{9})
		}
		os.Exit(0)
	}
	var output io.Writer = os.Stdout
	if mode == "lostack" {
		mode = "write"
		output = lostAckWriter{os.Stdout}
	}
	if err := RunPasteboardHelper(context.Background(), mode, name, os.Stdin, output); err != nil {
		os.Exit(92)
	}
	os.Exit(0)
}
func TestPrivateHelperCancellationKillsAndReaps(t *testing.T) {
	t.Setenv("BOXWARDEN_PRIVATE_TEST_AMBIENT", "must not reach child")
	name := fmt.Sprintf("org.boxwarden.test.uncooperative.%d.%d", os.Getpid(), time.Now().UnixNano())
	var command *exec.Cmd
	b := &processBoard{name: name, command: func(ctx context.Context, _, name string) (*exec.Cmd, error) {
		var err error
		command, err = privateHelperCommand(ctx, "hang", name)
		return command, err
	}}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := b.ReadText(ctx)
	if err != clipboardx.ErrCancelled || time.Since(start) > 300*time.Millisecond {
		t.Fatalf("cancel uncooperative child elapsed=%v err=%v", time.Since(start), err)
	}
	if command == nil || command.ProcessState == nil {
		t.Fatal("helper not reaped before return")
	}
}
func TestPrivateHelperWritePrecommitCancellationAndLostAcknowledgement(t *testing.T) {
	name := fmt.Sprintf("org.boxwarden.test.commit.%d.%d", os.Getpid(), time.Now().UnixNano())
	board := privateBoardForTest(name).(*processBoard)
	defer releasePrivateBoard(board)
	original := []byte("synthetic stable ☃\n")
	if out, err := board.WriteText(t.Context(), original); err != nil || out != clipboardx.Committed {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	board.beforeCommit = cancel
	out, err := board.WriteText(ctx, []byte("uncommitted replacement"))
	if out != clipboardx.Unchanged || err != clipboardx.ErrCancelled {
		t.Fatalf("precommit result %s %v", out, err)
	}
	board.beforeCommit = nil
	data, err := board.ReadText(t.Context())
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("precommit cancellation changed destination")
	}
	board.command = func(ctx context.Context, _, name string) (*exec.Cmd, error) {
		return privateHelperCommand(ctx, "lostack", name)
	}
	out, err = board.WriteText(t.Context(), []byte("committed without ack"))
	if out != clipboardx.Unknown || err != clipboardx.ErrUnknown {
		t.Fatalf("lost ack %s %v", out, err)
	}
	board.command = privateHelperCommand
	data, err = board.ReadText(t.Context())
	if err != nil || !bytes.Equal(data, []byte("committed without ack")) {
		t.Fatal("lost-ack fixture did not actually commit")
	}
}

func TestPrivateHelperRejectsMalformedOutputBeforeDestinationMutation(t *testing.T) {
	for _, mode := range []string{"oversized", "invalid", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			var command *exec.Cmd
			board := &processBoard{name: "org.boxwarden.test.framing", command: func(ctx context.Context, _ string, name string) (*exec.Cmd, error) {
				var err error
				command, err = privateHelperCommand(ctx, mode, name)
				return command, err
			}}
			data, err := board.ReadText(t.Context())
			expected := map[string]error{"oversized": clipboardx.ErrTooLarge, "invalid": clipboardx.ErrInvalidText, "trailing": clipboardx.ErrFrame}[mode]
			if err != expected || data != nil || command.ProcessState == nil {
				t.Fatalf("framing mode=%s err=%v reaped=%v", mode, err, command.ProcessState != nil)
			}
		})
	}
}
