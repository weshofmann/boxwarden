//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPhaseReservesBeforeEffectAndNoRetry(t *testing.T) {
	root := t.TempDir()
	w := testWindow()
	l, _ := newLedger(filepath.Join(root, "ledger"), w)
	a, _ := newArchive(filepath.Join(root, "archive"), w)
	r := clock.Reading{Wall: 101, Continuous: 110}
	b, _ := newBudget(w, r)
	e := &engine{window: w, ledger: l, archive: a, budget: b, now: func() (clock.Reading, error) { r.Wall++; r.Continuous++; return r, nil }}
	called := 0
	fn := func(ctx context.Context, id string) (any, int, error) {
		called++
		raw, err := os.ReadFile(filepath.Join(l.root, "reserved-control-create.json"))
		if err != nil || !strings.Contains(string(raw), id) {
			t.Fatal("effect before durable reservation")
		}
		return struct {
			Created bool `json:"created"`
		}{false}, 1, ErrRefused
	}
	if e.phase("control-create", false, fn) == nil || called != 1 {
		t.Fatal("unknown failure lost")
	}
	if e.phase("control-create", false, fn) == nil || called != 1 {
		t.Fatal("unknown retried")
	}
}
func TestStopNeedsTypedAcknowledgement(t *testing.T) {
	p := "domain: n1qualification\nsession: n1diag20260930control\nstate: stopped\nreadiness: not_ready\n"
	if stopAcknowledged([]byte(p), "n1diag20260930control") {
		t.Fatal("stored stopped accepted")
	}
	valid := p + "stop-outcome: request=guest_accepted forced=false workspace_cleanliness=unverified\n"
	if !stopAcknowledged([]byte(valid), "n1diag20260930control") {
		t.Fatal("actual renderer rejected")
	}
	for _, s := range []string{valid + "junk\n", strings.Replace(valid, "n1qualification", "foreign", 1), strings.Replace(valid, "guest_accepted", "arbitrary", 1), strings.Replace(valid, "forced=false", "forced=0", 1)} {
		if stopAcknowledged([]byte(s), "n1diag20260930control") {
			t.Fatal("malformed public ack")
		}
	}
}
func TestObserverBarrierRejectsFinalBufferedAndElapsed(t *testing.T) {
	w := sshx.N1PeerWatch{Version: 1, Domain: "n1qualification", Role: "control", Interface: "enp0s1", DurationMS: 30000}
	ready := sshx.N1PeerReady{Record: "ready", Version: 1, Domain: w.Domain, Role: w.Role, Interface: w.Interface, DurationMS: 30000, Binding: sshx.N1PeerPair{Candidate: w.Candidate, Control: w.Control}, IntervalOrigin: "ready_emit_start", ReadinessBudgetUS: 100000, ClosingToleranceUS: 250000}
	o := &observer{child: &retainedChild{finished: make(chan struct{})}, watch: w, ready: ready, started: clock.Reading{Wall: 100, Continuous: 100}, finalRead: make(chan struct{})}
	if !observerCurrent(o, clock.Reading{Wall: 101, Continuous: 101}) {
		t.Fatal("live ready rejected")
	}
	if observerCurrent(o, clock.Reading{Wall: 100 + 26e9, Continuous: 101}) {
		t.Fatal("spent interval extended")
	}
	close(o.finalRead)
	if observerCurrent(o, clock.Reading{Wall: 101, Continuous: 101}) {
		t.Fatal("buffered final rearmed")
	}
}
func TestStrictWorkerJSONRejectsFamilies(t *testing.T) {
	type r struct {
		Version int  `json:"version"`
		OK      bool `json:"ok"`
	}
	for _, s := range []string{`{"version":1,"ok":true,"ok":false}` + "\n", `{"version":1,"ok":true,"foreign":0}` + "\n", `{"version":1e0,"ok":true}` + "\n", `{"version":1,"ok":null}` + "\n", `{"version":99999999999999999999999,"ok":true}` + "\n", `{"ok":true,"version":1}` + "\n", `{"version":1,"ok":true}`} {
		if decode([]byte(s), &r{}, 16384) == nil {
			t.Fatal("bad record accepted", s)
		}
	}
	if decode([]byte(`{"version":1,"ok":true}`+"\n"), &r{}, 16384) != nil {
		t.Fatal("valid worker output")
	}
}
func TestRetainedChildFixture(t *testing.T) {
	// Only explicitly selected harmless fixture subprocesses enter this branch.
	for i, a := range os.Args {
		if a == "--n1-c-fixture" {
			switch os.Args[i+1] {
			case "argv":
				raw, _ := json.Marshal(os.Args[i+2:])
				os.Stdout.Write(raw)
			case "overflow":
				os.Stdout.Write(make([]byte, 20000))
			case "connected-error":
				os.Stdout.WriteString("{\"connected\":true}\n")
				os.Exit(7)
			case "sleep":
				time.Sleep(5 * time.Second)
			}
			os.Exit(0)
		}
	}
}
func fixtureArgs(kind string, args ...string) []string {
	return append([]string{"-test.run=^TestRetainedChildFixture$", "--", "--n1-c-fixture", kind}, args...)
}
func TestActualRetainedChildArgvEOFAndReap(t *testing.T) {
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	args := []string{"one argument with spaces", "$(literal)", "", "quote'byte"}
	r, e := callChild(ctx, p, fixtureArgs("argv", args...), nil, 4096)
	if e != nil || !r.closed || r.exit != 0 {
		t.Fatal(e, r)
	}
	var observed []string
	if json.Unmarshal(r.raw, &observed) != nil || strings.Join(observed, "\000") != strings.Join(args, "\000") {
		t.Fatal("child argv changed")
	}
	r, e = callChild(ctx, p, fixtureArgs("connected-error"), nil, 4096)
	if e == nil || r.exit != 7 || !r.closed || string(r.raw) != "{\"connected\":true}\n" {
		t.Fatal("fact/actual closure lost", r, e)
	}
}
func TestActualChildOverflowAndDeadlineRefuse(t *testing.T) {
	p, _ := os.Executable()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	r, e := callChild(ctx, p, fixtureArgs("overflow"), nil, 128)
	if e == nil || len(r.raw) != 0 {
		t.Fatal("overrun retained", r, e)
	}
	short, stop := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer stop()
	started := time.Now()
	_, e = callChild(short, p, fixtureArgs("sleep"), nil, 128)
	if e == nil || time.Since(started) > time.Second {
		t.Fatal("deadline did not close retained child")
	}
}
func TestArchiveRejectsChangedContentLinksAndModes(t *testing.T) {
	p := t.TempDir()
	a, _ := newArchive(filepath.Join(p, "archive"), testWindow())
	if a.write("network-verdict.json", []byte(`{"version":1}`)) != nil {
		t.Fatal("write")
	}
	os.WriteFile(filepath.Join(a.root, "network-verdict.json"), []byte(`{"version":2}`), 0600)
	if _, _, _, e := a.finalize(true, true); e == nil {
		t.Fatal("changed readback admitted")
	}
	leaf := filepath.Join(p, "leaf")
	os.WriteFile(leaf, []byte("x"), 0600)
	os.Link(leaf, filepath.Join(p, "other"))
	if _, e := privateRead(leaf, 16); e == nil {
		t.Fatal("hardlink admitted")
	}
	os.Remove(filepath.Join(p, "other"))
	os.Chmod(leaf, 0644)
	if _, e := privateRead(leaf, 16); e == nil {
		t.Fatal("mode drift admitted")
	}
}
func TestReadLineDoesNotOverallocate(t *testing.T) {
	if _, e := readLine(strings.NewReader(strings.Repeat("x", 1000000)), 512); e == nil {
		t.Fatal("unbounded line")
	}
	if _, e := readLine(io.LimitReader(strings.NewReader("a\n"), 1), 512); e == nil {
		t.Fatal("lost newline admitted")
	}
}
func TestRetainedJoinCanBeRepeatedWithoutAnotherWait(t *testing.T) {
	p, _ := os.Executable()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, e := startChild(ctx, p, fixtureArgs("argv", "x"), nil, 4096, nil)
	if e != nil {
		t.Fatal(e)
	}
	a := c.join()
	b := c.join()
	if !a.closed || !b.closed || a.exit != b.exit || string(a.raw) != string(b.raw) {
		t.Fatal("actual closure lost on repeated local join")
	}
}

func TestObserverSummaryUsesSummaryDiscriminator(t *testing.T) {
	w := sshx.N1PeerWatch{Version: 1, Domain: "n1qualification", Role: "control", Interface: "enp0s1", DurationMS: 30000}
	r := sshx.N1PeerReady{Record: "summary", Version: 1, Domain: w.Domain, Role: w.Role, Interface: w.Interface, DurationMS: 30000, Binding: sshx.N1PeerPair{Candidate: w.Candidate, Control: w.Control}, IntervalOrigin: "ready_emit_start", ReadinessBudgetUS: 100000, ClosingToleranceUS: 250000}
	s := sshx.N1PeerSummary{N1PeerReady: r, Complete: true, Ready: true, DeadlineReached: true, ElapsedMS: 30000}
	if !summaryComplete(s, w) {
		t.Fatal("actual summary discriminator refused")
	}
	s.Record = "ready"
	if summaryComplete(s, w) {
		t.Fatal("READY substituted for final summary")
	}
	s.Record = "summary"
	s.Binding.Candidate.Address = "foreign"
	if summaryComplete(s, w) {
		t.Fatal("foreign tuple admitted")
	}
}

func TestConnectCannotExtendEitherObserverOrArm(t *testing.T) {
	obs := [2]*observer{{started: clock.Reading{Wall: 100, Continuous: 100}}, {started: clock.Reading{Wall: 200, Continuous: 200}}}
	arm := networkdiag.ArmReceipt{Sent: networkdiag.ClockReading{WallNS: 300, ContinuousNS: 300}, Deadline: networkdiag.ClockReading{WallNS: 40e9, ContinuousNS: 40e9}}
	start := clock.Reading{Wall: 400, Continuous: 400}
	if !connectEnvelope(obs, arm, start, clock.Reading{Wall: 500, Continuous: 500}) {
		t.Fatal("bounded exact interval refused")
	}
	for _, end := range []clock.Reading{{Wall: 30e9 + 100, Continuous: 500}, {Wall: 500, Continuous: 30e9 + 100}, {Wall: 399, Continuous: 500}} {
		if connectEnvelope(obs, arm, start, end) {
			t.Fatal("observer interval or regression extended")
		}
	}
	arm.Deadline = networkdiag.ClockReading{WallNS: 500, ContinuousNS: 500}
	if connectEnvelope(obs, arm, start, clock.Reading{Wall: 500, Continuous: 500}) {
		t.Fatal("ARM send deadline extended")
	}
}
