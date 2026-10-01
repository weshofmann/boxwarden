//go:build n1clipboarddiagnostic && !n1candidate

package sshx

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN1ConnectPreservesCompletedFactWithTransportFailure(t *testing.T) {
	c := NewClient(&fakeRunner{onRun: func(Command) Result {
		return Result{Stdout: `{"version":1,"binding":{"version":1,"domain":"n1qualification","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","generation":"123e4567-e89b-42d3-a456-426614174001"},"control_ipv4":"192.168.64.2","code":"connected","errno":0,"elapsed_us":1,"connected":true,"close_ok":true,"timing_ok":true}`, Truncated: true}
	}})
	conn, b := n1Fixture(t)
	result, err := c.ConnectN1Peer(context.Background(), conn, N1ConnectRequest{Binding: b, ControlIPv4: "192.168.64.2"})
	if err == nil || !result.Connected {
		t.Fatalf("lost completed connect or transport refusal: %+v %v", result, err)
	}
}

func TestN1RejectsForeignBindingIPv6AndStalePinBeforeDispatch(t *testing.T) {
	runner := &fakeRunner{}
	c := NewClient(runner)
	conn, b := n1Fixture(t)
	bad := b
	bad.Domain = "work"
	if _, err := c.RunN1Controls(context.Background(), conn, bad); err == nil {
		t.Fatal("foreign binding accepted")
	}
	conn.Address = "::1"
	if _, err := c.RunN1Controls(context.Background(), conn, b); err == nil {
		t.Fatal("IPv6 accepted")
	}
	conn.Address = "192.168.64.3"
	mustWrite(t, conn.KnownHostsFile, []byte("stale"), 0600)
	if _, err := c.RunN1Controls(context.Background(), conn, b); err == nil {
		t.Fatal("stale pin accepted")
	}
	if len(runner.commands) != 0 {
		t.Fatal("invalid input dispatched")
	}
}

func TestN1FixedSourceSurvivesActualRemoteShellInterpretation(t *testing.T) {
	conn, _ := n1Fixture(t)
	args := n1Arguments(conn, n1StagerSource, true)
	if len(args)+1 != 72 {
		t.Fatalf("argc=%d", len(args)+1)
	}
	suffix := args[66:]
	if len(suffix) != 5 || suffix[0] != "/usr/bin/python3" || suffix[3] != "-c" {
		t.Fatalf("fixed remote args: %v", suffix[:4])
	}
	// Local harmless shell models OpenSSH's unavoidable second interpretation.
	// The spy is Python, not the guest stager; it records the actual recovered argv.
	spy := `import json,sys; print(json.dumps(sys.argv[1:]))`
	remote := "/usr/bin/python3 -I -S -c " + n1ShellQuote(spy) + " " + strings.Join(suffix, " ")
	out, err := exec.Command("/bin/sh", "-c", remote).Output()
	if err != nil {
		t.Fatal(err)
	}
	var observed []string
	if json.Unmarshal(out, &observed) != nil || len(observed) != 5 || observed[4] != n1StagerSource {
		t.Fatal("second interpretation changed source bytes/count")
	}
}

func TestN1OwnedProcessOverflowDeadlineAndActualExit(t *testing.T) {
	for _, entry := range []struct {
		code    string
		limit   int
		wantErr bool
		exit    int
	}{
		{`import sys;sys.stdout.write("x"*4097)`, 4096, true, 0},
		{`import time;time.sleep(2)`, 4096, true, -1},
		{`import sys;sys.stdout.write("ok");sys.exit(7)`, 4096, false, 7},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		result, err := n1Execute(ctx, Command{Path: "/usr/bin/python3", Args: []string{"-B", "-I", "-S", "-c", entry.code}}, entry.limit, nil)
		cancel()
		if (err != nil) != entry.wantErr || (!entry.wantErr && result.exit != entry.exit) {
			t.Fatalf("exit/limit refusal=%+v %v", result, err)
		}
		if err != nil && len(result.output) != 0 {
			t.Fatal("failure retained raw output")
		}
	}
}

func TestN1ObserverReadyPrecedesFinalAndDuplicateRefuses(t *testing.T) {
	w := n1WatchFixture()
	ready := n1ReadyFixture(w)
	line, _ := json.Marshal(ready)
	seen := false
	parser := n1PeerParser{watch: w, onReady: func(N1PeerReady) error { seen = true; return nil }}
	if err := parser.record(line); err != nil || !seen {
		t.Fatalf("READY not admitted: %v", err)
	}
	if err := parser.record(line); err == nil {
		t.Fatal("duplicate READY accepted")
	}
	parser = n1PeerParser{watch: w, onReady: func(N1PeerReady) error { return nil }}
	if err := parser.record([]byte(`{"record":"summary"}`)); err == nil {
		t.Fatal("summary before READY accepted")
	}
}

func TestN1StrictResultRejectsUnknownDuplicateNullAndOversize(t *testing.T) {
	for _, raw := range [][]byte{[]byte(`{"version":1,"version":1}`), []byte(`null`), []byte(`{"version":1,"extra":"secret"}`), bytes.Repeat([]byte("x"), 4097)} {
		var value N1ControlsResult
		if n1Decode(raw, &value, 4096) == nil {
			t.Fatal("ambiguous metadata admitted")
		}
	}
}

func n1Fixture(t *testing.T) (Connection, N1GuestBinding) {
	conn := testConnection(t)
	conn.Address = "192.168.64.3"
	conn.Binding.Domain = "n1qualification"
	conn.Pin.Domain = "n1qualification"
	mustWrite(t, conn.KnownHostsFile, []byte(HostKeyAlias(conn.Binding.SessionID)+" "+conn.Pin.PublicKey+"\n"), 0600)
	return conn, N1GuestBinding{Version: 1, Domain: "n1qualification", SessionID: conn.Binding.SessionID, BackendKind: conn.Binding.BackendKind, BackendObject: conn.Binding.BackendObject, Generation: "123e4567-e89b-42d3-a456-426614174001"}
}
func n1WatchFixture() N1PeerWatch {
	return N1PeerWatch{Version: 1, Domain: "n1qualification", Role: "candidate", Interface: "enp0s1", Gateway: "192.168.64.1", DurationMS: 30000, Candidate: N1Peer{SessionUUID: testUUID, GenerationUUID: "123e4567-e89b-42d3-a456-426614174001", Backend: "candidate", Address: "192.168.64.3", MAC: "02:00:00:00:00:03"}, Control: N1Peer{SessionUUID: "123e4567-e89b-42d3-a456-426614174002", GenerationUUID: "123e4567-e89b-42d3-a456-426614174003", Backend: "control", Address: "192.168.64.2", MAC: "02:00:00:00:00:02"}}
}
func n1ReadyFixture(w N1PeerWatch) N1PeerReady {
	return N1PeerReady{Record: "ready", Version: 1, Domain: w.Domain, Role: w.Role, Interface: w.Interface, DurationMS: w.DurationMS, Gateway: w.Gateway, Binding: N1PeerPair{Candidate: w.Candidate, Control: w.Control}, IntervalOrigin: "ready_emit_start", ReadinessBudgetUS: 100000, ClosingToleranceUS: 250000}
}

func TestN1ObserverAdmitsCanonicalSummaryAndRejectsUnknownCounter(t *testing.T) {
	w := n1WatchFixture()
	r := n1ReadyFixture(w)
	s := N1PeerSummary{N1PeerReady: r, ReadinessDelayUS: 1, CoverageScope: "identified_pair_headers", Ready: true, DeadlineReached: true, ElapsedMS: 30000, Complete: true, IncompleteReasons: []string{}, Counters: map[string]int{}, RouteBefore: N1RouteState{Status: "ok", DeviceMatches: true, SourceMatches: true}, RouteAfter: N1RouteState{Status: "ok", DeviceMatches: true, SourceMatches: true}, NeighborBefore: N1NeighborState{Status: "absent", State: "NONE"}, NeighborAfter: N1NeighborState{Status: "absent", State: "NONE"}, NegativeInference: N1PeerInference{Scope: "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap", CandidatePreEmissionAbsenceIfDriverControlsPass: true, DriverControlsRequired: true, QualifiedGuestCaptureRequired: true}}
	s.Record = "summary"
	for _, key := range n1CounterCodes {
		s.Counters[key] = 0
	}
	line, _ := json.Marshal(s)
	parser := n1PeerParser{watch: w, onReady: func(N1PeerReady) error { return nil }, ready: true}
	if err := parser.record(line); err != nil {
		t.Fatalf("canonical final refused: %v", err)
	}
	s.Counters["secret_counter"] = 0
	line, _ = json.Marshal(s)
	parser.final = false
	if parser.record(line) == nil {
		t.Fatal("unknown counter admitted")
	}
}

func TestN1ActualHeldDescendantEOFAndStderrOverflowRefuse(t *testing.T) {
	for _, code := range []string{
		`import os,time; pid=os.fork(); time.sleep(0.3) if pid==0 else None`,
		`import sys;sys.stdout.write("ok");sys.stderr.write("SECRET"*1000)`,
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		result, err := n1Execute(ctx, Command{Path: "/usr/bin/python3", Args: []string{"-B", "-I", "-S", "-c", code}}, 4096, nil)
		cancel()
		if err == nil || len(result.output) != 0 {
			t.Fatal("missing EOF/overflow accepted or bytes retained")
		}
	}
}

func TestN1ConnectReceiptRetainedBeforeHeldEOF(t *testing.T) {
	conn, b := n1Fixture(t)
	received := N1ConnectResult{Version: 1, Binding: b, Code: "connected", ControlIPv4: "192.168.64.2", Connected: true, CloseOK: true, TimingOK: true}
	raw, _ := json.Marshal(received)
	q, _ := json.Marshal(string(raw) + "\n")
	code := "import os,time,sys;sys.stdout.write(" + string(q) + ");sys.stdout.flush();pid=os.fork();time.sleep(0.3) if pid==0 else None"
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var result N1ConnectResult
	_, err := n1Execute(ctx, Command{Path: "/usr/bin/python3", Args: []string{"-B", "-I", "-S", "-c", code}}, 4096, func(r io.Reader) error {
		return n1ReadSingleRecord(r, 4096, func(raw []byte) error { return n1Decode(raw, &result, 4096) })
	})
	if err == nil || !result.Connected {
		t.Fatal("completed connection lost to descendant EOF failure")
	}
	_ = conn
}

func TestN1StageFrameConsumedByFixedPythonParser(t *testing.T) {
	_, b := n1Fixture(t)
	descriptors := make([]n1ArtifactDescriptor, 9)
	values := make([][]byte, 9)
	for i, name := range n1Names {
		values[i] = []byte("fixture-" + name)
		descriptors[i] = n1ArtifactDescriptor{name, len(values[i]), n1Hash(values[i])}
	}
	raw, err := n1StageFrame(b, descriptors, values)
	if err != nil {
		t.Fatal(err)
	}
	code := `import importlib.util,json,sys; s=importlib.util.spec_from_file_location('fixed','n1guest/stager.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);h,v=m.read_frame(sys.stdin.buffer);print(json.dumps({'count':len(v),'session':h['binding']['session_id'],'first':v[0].decode()}))`
	cmd := exec.Command("/usr/bin/python3", "-B", "-I", "-S", "-c", code)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Count   int    `json:"count"`
		Session string `json:"session"`
		First   string `json:"first"`
	}
	if json.Unmarshal(out, &observed) != nil || observed.Count != 9 || observed.Session != testUUID || observed.First != "fixture-boxwarden-guest-bootstrap" {
		t.Fatal("actual parser changed fixed frame")
	}
}

func TestN1StageRejectsUnadmittedArtifactsWithoutDispatch(t *testing.T) {
	conn, b := n1Fixture(t)
	runner := &fakeRunner{}
	client := NewClient(runner)
	h := N1GuestSourceHashes()
	h.TrialHelper = strings.Repeat("1", 64)
	h.Overlay = strings.Repeat("2", 64)
	if _, err := client.StageN1Guest(context.Background(), conn, b, N1GuestArtifacts{}, h); err == nil {
		t.Fatal("missing artifacts admitted")
	}
	if _, err := client.InspectN1Guest(context.Background(), conn, N1InspectRequest{Binding: b, Phase: N1GuestFinal, Hashes: h}); err == nil {
		t.Fatal("unadmitted inspector artifact metadata accepted")
	}
	if len(runner.commands) != 0 {
		t.Fatal("unadmitted source dispatched")
	}
}

type n1LocalPeerRunner struct{ code string }

func (n1LocalPeerRunner) Run(context.Context, Command) (Result, error) {
	panic("buffered runner cannot implement READY barrier")
}
func (r n1LocalPeerRunner) runN1Peer(ctx context.Context, cmd Command, consume func(io.Reader) error) (n1ProcessResult, error) {
	return n1Execute(ctx, Command{Path: "/usr/bin/python3", Args: []string{"-B", "-I", "-S", "-c", r.code}}, 16384, consume)
}

func TestN1ObserveActualReadyCallbackErrorAndPartialStreamTerminate(t *testing.T) {
	w := n1WatchFixture()
	conn, _ := n1Fixture(t)
	conn.Binding.BackendObject = w.Candidate.Backend
	conn.Pin.BackendObject = w.Candidate.Backend
	line, _ := json.Marshal(n1ReadyFixture(w))
	quoted, _ := json.Marshal(string(line) + "\n")
	for _, entry := range []struct {
		code        string
		callbackErr bool
	}{
		{"import sys,time;sys.stdout.write(" + string(quoted) + ");sys.stdout.flush();time.sleep(2)", true},
		{"import sys;sys.stdout.write(" + string(quoted) + ");sys.stdout.write('{')", false},
		{"import sys;sys.stdout.write(" + string(quoted) + ");sys.stderr.write('SECRET')", false},
	} {
		client := NewClient(n1LocalPeerRunner{entry.code})
		called := 0
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		_, err := client.ObserveN1Peer(ctx, conn, w, func(r N1PeerReady) error {
			called++
			if entry.callbackErr {
				return ErrN1Guest
			}
			return nil
		})
		cancel()
		if err == nil || called != 1 {
			t.Fatalf("incomplete observer admitted/callback order lost: %v %d", err, called)
		}
	}
}

func TestN1ActualObserverCanonicalMetadataAndMaximumWidthRemainBounded(t *testing.T) {
	w := n1WatchFixture()
	w.Interface = "e12345678901234"
	w.Candidate.Backend = strings.Repeat("c", 64)
	w.Control.Backend = strings.Repeat("p", 64)
	w.Candidate.Address = "192.168.255.255"
	w.Control.Address = "192.168.255.254"
	w.Gateway = "192.168.255.253"
	raw, _ := json.Marshal(w)
	quoted, _ := json.Marshal(string(raw))
	code := `import importlib.util,json,sys; s=importlib.util.spec_from_file_location('observer','../../tools/n1-qualification/guest_metadata_observer.py');m=importlib.util.module_from_spec(s);s.loader.exec_module(m);w=m.parse_watch(` + string(quoted) + `.encode());r=m._ready_record(w);state={'route_before':{'status':'unavailable','device_matches':False,'source_matches':False,'gateway_matches':False,'metric':2147483647},'route_after':{'status':'unavailable','device_matches':False,'source_matches':False,'gateway_matches':False,'metric':2147483647},'neighbor_before':{'status':'unavailable','state':'INCOMPLETE','mac_matches':False},'neighbor_after':{'status':'unavailable','state':'INCOMPLETE','mac_matches':False}};s=m._summary_record(w,ready=False,complete=False,deadline_reached=False,elapsed_ms=60000,reasons=set(m.INCOMPLETE_CODES),counters={k:4096 for k in m.EVENT_CODES},drops=4294967295,packets=4097,states=state,overflow=False,readiness_delay_us=60000000); print(json.dumps(r,separators=(',',':')));print(json.dumps(s,separators=(',',':')))`
	out, err := exec.Command("/usr/bin/python3", "-B", "-I", "-S", "-c", code).Output()
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	if len(lines) != 2 || len(lines[0])+1 > 8192 || len(lines[1])+1 > 8192 {
		t.Fatal("canonical observer exceeded bounds")
	}
	parser := n1PeerParser{watch: w, onReady: func(N1PeerReady) error { return nil }}
	for _, line := range lines {
		if parser.record(line) != nil {
			t.Fatal("canonical Python schema incompatible with Go")
		}
	}
	if !parser.final || parser.summary.Complete {
		t.Fatal("incomplete counter fixture promoted")
	}
	t.Logf("conservative synthetic ready/final bytes including LF=%d/%d", len(lines[0])+1, len(lines[1])+1)
}

func TestN1ObserverRealChildRequiresEarlyReadyCallbackBeforeFinal(t *testing.T) {
	w := n1WatchFixture()
	conn, _ := n1Fixture(t)
	conn.Binding.BackendObject = w.Candidate.Backend
	conn.Pin.BackendObject = w.Candidate.Backend
	ready := n1ReadyFixture(w)
	summary := N1PeerSummary{N1PeerReady: ready, CoverageScope: "identified_pair_headers", IncompleteReasons: []string{"capture_error"}, Counters: map[string]int{}, RouteBefore: N1RouteState{Status: "invalid"}, RouteAfter: N1RouteState{Status: "invalid"}, NeighborBefore: N1NeighborState{Status: "invalid", State: "UNKNOWN"}, NeighborAfter: N1NeighborState{Status: "invalid", State: "UNKNOWN"}, NegativeInference: N1PeerInference{Scope: "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap", DriverControlsRequired: true, QualifiedGuestCaptureRequired: true}}
	summary.Record = "summary"
	for _, key := range n1CounterCodes {
		summary.Counters[key] = 0
	}
	r, _ := json.Marshal(ready)
	s, _ := json.Marshal(summary)
	qr, _ := json.Marshal(string(r) + "\n")
	qs, _ := json.Marshal(string(s) + "\n")
	marker := filepath.Join(privateRoot(t), "callback acknowledged")
	qm, _ := json.Marshal(marker)
	code := "import sys,time,os;sys.stdout.write(" + string(qr) + ");sys.stdout.flush()\nfor _ in range(50):\n if os.path.exists(" + string(qm) + "):break\n time.sleep(.02)\nelse:sys.exit(9)\nsys.stdout.write(" + string(qs) + ");sys.stdout.flush();sys.exit(1)"
	client := NewClient(n1LocalPeerRunner{code})
	called := false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := client.ObserveN1Peer(ctx, conn, w, func(N1PeerReady) error { called = true; return os.WriteFile(marker, []byte("synthetic ready"), 0600) })
	if err != nil || !called || result.Record != "summary" || result.Complete {
		t.Fatalf("early barrier/final incomplete receipt lost: %+v %v", result, err)
	}
}

func TestN1ActualLocalChildArgvCountAndBytesPreserveSpaces(t *testing.T) {
	conn, _ := n1Fixture(t)
	conn.RuntimeDirectory = "/synthetic runtime with spaces"
	conn.IdentityFile = conn.RuntimeDirectory + "/client"
	conn.KnownHostsFile = conn.RuntimeDirectory + "/known_hosts"
	args := n1Arguments(conn, n1StagerSource, true)
	code := `import sys,json;print(json.dumps([x.encode().hex() for x in sys.argv[1:]]))`
	command := exec.Command("/usr/bin/python3", append([]string{"-B", "-I", "-S", "-c", code}, args...)...)
	raw, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var argv []string
	if json.Unmarshal(raw, &argv) != nil || len(argv) != 71 {
		t.Fatal("local argv element count changed")
	}
	if argv[3] != hex.EncodeToString([]byte(`IdentityFile="/synthetic runtime with spaces/client"`)) || argv[7] != hex.EncodeToString([]byte(`UserKnownHostsFile="/synthetic runtime with spaces/known_hosts"`)) || argv[70] != hex.EncodeToString([]byte(n1ShellQuote(n1StagerSource))) {
		t.Fatal("local argv element bytes changed")
	}
}

func TestN1ActualOwnedReadCloseFailureRefuses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := n1Execute(ctx, Command{Path: "/usr/bin/python3", Args: []string{"-B", "-I", "-S", "-c", `print("synthetic")`}}, 4096, func(r io.Reader) error {
		if _, e := n1ReadBounded(r, 4096); e != nil {
			return e
		}
		// The test closes the actual retained read handle first. The production
		// checked close must report failure; namespace/EOF cannot reconstruct it.
		return r.(*os.File).Close()
	})
	if err == nil {
		t.Fatal("failed actual checked-close became success")
	}
}

func TestN1AbsentExecDependencyRefusesBeforeLiveRunnerSelection(t *testing.T) {
	conn, b := n1Fixture(t)
	for _, runner := range []Runner{ExecRunner{}, (*ExecRunner)(nil), &ExecRunner{}} {
		if NewClient(runner).n1Admit(context.Background(), conn, b) == nil {
			t.Fatal("absent exec dependency can select live transport")
		}
	}
}

// The receiver sees rounded metadata, not the producer's underlying clock.
// These literal boundaries include both representations of a nominal half-ms
// closing endpoint when subtraction from a nonzero origin shifts the tie.
func TestN1ObserverCompleteTimingAdmission(t *testing.T) {
	cases := []struct {
		name                        string
		duration, budget, tolerance int
		delay, elapsed              int64
		admitted                    bool
	}{
		{"long-readiness-and-close-boundary", 30000, 100000, 250000, 100000, 30250, true},
		{"readiness-over-budget", 30000, 100000, 250000, 100001, 30000, false},
		{"readiness-exhausts-interval", 30000, 100000, 250000, 30000000, 30000, false},
		{"review-readiness-counterexample", 30000, 100000, 250000, 60000000, 30000, false},
		{"closing-beyond-tolerance", 30000, 100000, 250000, 1, 30251, false},
		{"review-closing-counterexample", 30000, 100000, 250000, 1, 60000, false},
		{"before-deadline", 30000, 100000, 250000, 1, 29999, false},
		{"one-ms-quarter-boundary", 1, 250, 250, 250, 1, true},
		{"one-ms-readiness-over-budget", 1, 250, 250, 251, 1, false},
		{"one-ms-closing-over-boundary", 1, 250, 250, 1, 2, false},
		{"two-ms-half-round-even", 2, 500, 500, 500, 2, true},
		{"two-ms-half-float-origin", 2, 500, 500, 1, 3, true},
		{"two-ms-closing-over-boundary", 2, 500, 500, 1, 4, false},
		{"three-ms-three-quarter-round-up", 3, 750, 750, 750, 4, true},
		{"three-ms-closing-over-boundary", 3, 750, 750, 1, 5, false},
		{"four-ms-integral-boundary", 4, 1000, 1000, 1000, 5, true},
		{"six-ms-half-round-even-up", 6, 1500, 1500, 1500, 8, true},
		{"six-ms-closing-over-boundary", 6, 1500, 1500, 1, 9, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := n1WatchFixture()
			w.DurationMS = tc.duration
			s := n1TimingSummary(w)
			s.ReadinessBudgetUS, s.ClosingToleranceUS = tc.budget, tc.tolerance
			s.ReadinessDelayUS, s.ElapsedMS = tc.delay, tc.elapsed
			if got := s.valid(w); got != tc.admitted {
				t.Errorf("complete timing admission = %v, want %v", got, tc.admitted)
			}
			conn, _ := n1Fixture(t)
			conn.Binding.BackendObject, conn.Pin.BackendObject = w.Candidate.Backend, w.Candidate.Backend
			r := s.N1PeerReady
			r.Record = "ready"
			ready, _ := json.Marshal(r)
			final, _ := json.Marshal(s)
			stream, _ := json.Marshal(string(ready) + "\n" + string(final) + "\n")
			client := NewClient(n1LocalPeerRunner{"import sys;sys.stdout.write(" + string(stream) + ");sys.stdout.flush()"})
			called := 0
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := client.ObserveN1Peer(ctx, conn, w, func(N1PeerReady) error { called++; return nil })
			if called != 1 || (err == nil) != tc.admitted || result.Complete != tc.admitted || result.NegativeInference.CandidatePreEmissionAbsenceIfDriverControlsPass != tc.admitted {
				t.Fatalf("typed stream admission/eligibility differs: called=%d complete=%v eligible=%v err=%v", called, result.Complete, result.NegativeInference.CandidatePreEmissionAbsenceIfDriverControlsPass, err)
			}
		})
	}
}

func n1TimingSummary(w N1PeerWatch) N1PeerSummary {
	s := N1PeerSummary{N1PeerReady: n1ReadyFixture(w), ReadinessDelayUS: 1, CoverageScope: "identified_pair_headers", Ready: true, DeadlineReached: true, ElapsedMS: int64(w.DurationMS), Complete: true, IncompleteReasons: []string{}, Counters: map[string]int{}, RouteBefore: N1RouteState{Status: "ok", DeviceMatches: true, SourceMatches: true}, RouteAfter: N1RouteState{Status: "ok", DeviceMatches: true, SourceMatches: true}, NeighborBefore: N1NeighborState{Status: "absent", State: "NONE"}, NeighborAfter: N1NeighborState{Status: "absent", State: "NONE"}, NegativeInference: N1PeerInference{Scope: "supported_well_formed_outgoing_candidate_arp_or_tcp22_syn_at_guest_tap", CandidatePreEmissionAbsenceIfDriverControlsPass: true, DriverControlsRequired: true, QualifiedGuestCaptureRequired: true}}
	s.Record = "summary"
	for _, code := range n1CounterCodes {
		s.Counters[code] = 0
	}
	return s
}

func TestN1ObserverIncompleteTimingMetadataRetained(t *testing.T) {
	for _, reason := range []string{"readiness_late", "observer_interval_overrun"} {
		t.Run(reason, func(t *testing.T) {
			w := n1WatchFixture()
			s := n1TimingSummary(w)
			s.Ready, s.Complete = false, false
			s.ReadinessDelayUS, s.ElapsedMS = 60000000, 60000
			s.IncompleteReasons = []string{reason}
			s.NegativeInference.CandidatePreEmissionAbsenceIfDriverControlsPass = false
			if !s.valid(w) {
				t.Fatal("bounded incomplete timing metadata refused")
			}
			conn, _ := n1Fixture(t)
			conn.Binding.BackendObject, conn.Pin.BackendObject = w.Candidate.Backend, w.Candidate.Backend
			r := s.N1PeerReady
			r.Record = "ready"
			ready, _ := json.Marshal(r)
			final, _ := json.Marshal(s)
			stream, _ := json.Marshal(string(ready) + "\n" + string(final) + "\n")
			client := NewClient(n1LocalPeerRunner{"import sys;sys.stdout.write(" + string(stream) + ");sys.stdout.flush();sys.exit(1)"})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			result, err := client.ObserveN1Peer(ctx, conn, w, func(N1PeerReady) error { return nil })
			if err != nil || result.Complete || result.ReadinessDelayUS != 60000000 || result.ElapsedMS != 60000 || len(result.IncompleteReasons) != 1 || result.IncompleteReasons[0] != reason {
				t.Fatal("incomplete receipt metadata lost")
			}
		})
	}
}
