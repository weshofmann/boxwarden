//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/adjudicate"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"io"
	"net"
	"time"
)

type observer struct {
	child     *retainedChild
	attempt   contract.Attempt
	watch     sshx.N1PeerWatch
	started   clock.Reading
	ready     sshx.N1PeerReady
	summary   sshx.N1PeerSummary
	finalRead chan struct{}
}

func peer(o worker.RuntimeObservation) sshx.N1Peer {
	return sshx.N1Peer{SessionUUID: o.Identity.Session, GenerationUUID: o.Identity.Generation, Backend: o.Identity.Backend, Address: net.IP(o.Network.Binding.Address[:]).String(), MAC: net.HardwareAddr(o.Network.Binding.MAC[:]).String()}
}
func readyMatches(r sshx.N1PeerReady, w sshx.N1PeerWatch) bool {
	return r.Record == "ready" && r.Version == 1 && r.Domain == w.Domain && r.Role == w.Role && r.Interface == w.Interface && r.Gateway == w.Gateway && r.DurationMS == 30000 && r.Binding == (sshx.N1PeerPair{Candidate: w.Candidate, Control: w.Control}) && r.IntervalOrigin == "ready_emit_start" && r.ReadinessBudgetUS == 100000 && r.ClosingToleranceUS == 250000
}
func summaryComplete(s sshx.N1PeerSummary, w sshx.N1PeerWatch) bool {
	if s.Record != "summary" {
		return false
	}
	r := s.N1PeerReady
	r.Record = "ready"
	return readyMatches(r, w) && s.Complete && s.Ready && s.DeadlineReached && !s.Overflow && s.Drops == 0 && len(s.IncompleteReasons) == 0 && s.ElapsedMS >= 30000 && s.ElapsedMS <= 30250 && s.ReadinessDelayUS >= 0 && s.ReadinessDelayUS <= 100000
}
func (e *engine) beginObserver(ctx context.Context, i int, w sshx.N1PeerWatch) (*observer, error) {
	if e.closed || e.check(false) != nil {
		return nil, ErrRefused
	}
	a, err := e.ledger.reserve("observer-" + role(i))
	if err != nil {
		return nil, err
	}
	start, err := e.now()
	if err != nil {
		return nil, ErrRefused
	}
	o := &observer{attempt: a, watch: w, started: start, finalRead: make(chan struct{})}
	ready := make(chan sshx.N1PeerReady, 1)
	req := worker.Request{Version: 1, Window: e.window, Identity: e.pair[i].Identity, Watch: &w}
	input, _ := json.Marshal(req)
	input = append(input, '\n')
	if e.executable(i+2) != nil {
		e.localDirty = true
		return nil, ErrRefused
	}
	child, err := startChild(ctx, contract.StaticFilePath(i+2), []string{"observe"}, input, 8192, func(r io.Reader) ([]byte, error) {
		line, err := readLine(r, 8192)
		var v sshx.N1PeerReady
		if err != nil || decode(line, &v, 8192) != nil || !readyMatches(v, w) {
			return nil, ErrRefused
		}
		ready <- v
		final, err := readLine(r, 8192)
		if err != nil || decode(final, &o.summary, 8192) != nil {
			return nil, ErrRefused
		}
		close(o.finalRead)
		var tail [1]byte
		n, err := r.Read(tail[:])
		if n != 0 || err != io.EOF {
			return nil, ErrRefused
		}
		return final, nil
	})
	if err != nil {
		e.localDirty = true
		return nil, err
	}
	o.child = child
	wait, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	select {
	case v := <-ready:
		o.ready = v
		return o, nil
	case <-wait.Done():
		child.cancel()
		return o, ErrRefused
	case <-child.finished:
		return o, ErrRefused
	}
}
func (e *engine) endObserver(o *observer) (sshx.N1PeerSummary, bool, error) {
	if o == nil || o.child == nil {
		return sshx.N1PeerSummary{}, false, ErrRefused
	}
	r := o.child.join()
	clockOK := e.check(false) == nil
	complete := r.closed && r.exit == 0 && !r.stderr && summaryComplete(o.summary, o.watch) && readyMatches(o.ready, o.watch)
	if !r.closed {
		e.localDirty = true
	}
	if e.ledger.closeAttempt(o.attempt.ID) != nil {
		return o.summary, false, ErrRefused
	}
	o.attempt.Closed = true
	status := "complete"
	if !complete {
		status = "unknown"
	}
	raw, err := contract.Encode(phaseReceipt{1, e.window.ID, o.attempt, status, 1, phaseEnvelope{o.started, e.budget.last}, []commandWitness{argvWitness(contract.StaticFilePath(roleIndex(o.watch.Role)+2), []string{"observe"})}, o.summary})
	if err != nil || e.archive.write("receipt-"+o.attempt.Command+".json", raw) != nil || !r.closed || !clockOK {
		return o.summary, false, ErrRefused
	}
	return o.summary, complete, nil
}
func observerCurrent(o *observer, r clock.Reading) bool {
	if o == nil || o.child == nil || r.Wall < o.started.Wall || r.Continuous < o.started.Continuous || r.Wall-o.started.Wall >= 26*1000000000 || r.Continuous-o.started.Continuous >= 26*1000000000 {
		return false
	}
	select {
	case <-o.finalRead:
		return false
	default:
	}
	select {
	case <-o.child.finished:
		return false
	default:
	}
	return readyMatches(o.ready, o.watch)
}
func (e *engine) positives(name string) (bool, error) {
	good := true
	err := e.phase(name, false, func(ctx context.Context, id string) (any, int, error) {
		for i := 0; i < 2; i++ {
			var r struct {
				Version int  `json:"version"`
				OK      bool `json:"ok"`
			}
			if e.worker(ctx, i, "positive", e.pair[i].Identity, nil, &r) != nil || r.Version != 1 || !r.OK {
				good = false
			}
		}
		return struct {
			Both bool `json:"both"`
		}{good}, 2, nil
	})
	return good, err
}
func (e *engine) controls(name string) (bool, error) {
	good := true
	err := e.phase(name, false, func(ctx context.Context, id string) (any, int, error) {
		var out [2]sshx.N1ControlsResult
		for i := 0; i < 2; i++ {
			if e.worker(ctx, i, "controls", e.pair[i].Identity, nil, &out[i]) != nil || out[i].DNS != "ok" || out[i].HTTPS != "ok" {
				good = false
			}
		}
		return out, 2, nil
	})
	return good, err
}
func (e *engine) network() error {
	var facts adjudicate.NetworkEvidence
	defer func() {
		if facts.ConnectValid && facts.Connected {
			exists := false
			for _, entry := range e.archive.entries {
				if entry.Name == "network-verdict.json" {
					exists = true
				}
			}
			if !exists {
				_ = e.networkVerdict(facts)
			}
		}
	}()
	before, err := e.controls("network-controls-before")
	if err != nil {
		return err
	}
	facts.ControlsBefore = before
	positive, err := e.positives("positive-before")
	if err != nil {
		return err
	}
	facts.PositiveBefore = positive
	// A failed prerequisite closes this diagnostic interval. It never creates an
	// observer, ARM or replacement connect to manufacture missing coverage.
	if !before || !positive {
		return e.networkVerdict(facts)
	}
	p := e.pair
	inspectCtx, inspectCancel := context.WithDeadline(context.Background(), e.deadline(false))
	defer inspectCancel()
	if !distinct(p) {
		return ErrRefused
	}
	for i := 0; i < 2; i++ {
		var v worker.RuntimeObservation
		if e.worker(inspectCtx, i, "inspect", p[i].Identity, nil, &v) != nil || admitRuntime(v, i, p[i].Identity) != nil {
			return e.networkVerdict(facts)
		}
		e.pair[i] = v
	}
	p = e.pair
	if !distinct(p) {
		return ErrRefused
	}
	gateway := net.IP(p[1].Launch.Gateway[:]).String()
	base := sshx.N1PeerWatch{Version: 1, Domain: "n1qualification", Interface: "enp0s1", Candidate: peer(p[1]), Control: peer(p[0]), Gateway: gateway, DurationMS: 30000}
	ctx, cancel := context.WithDeadline(context.Background(), e.deadline(false))
	ctx, shortCancel := context.WithTimeout(ctx, 35*time.Second)
	defer shortCancel()
	defer cancel()
	var obs [2]*observer
	var summaries [2]sshx.N1PeerSummary
	joined := false
	defer func() {
		if !joined {
			for _, o := range obs {
				if o != nil && o.child != nil {
					o.child.cancel()
					o.child.join()
				}
			}
		}
	}()
	for i := 0; i < 2; i++ {
		w := base
		w.Role = role(i)
		obs[i], err = e.beginObserver(ctx, i, w)
		if err != nil {
			for j, o := range obs {
				if o != nil && o.child != nil {
					o.child.cancel()
					summaries[j], _, _ = e.endObserver(o)
				}
			}
			joined = true
			return e.networkVerdict(facts)
		}
	}
	var arm networkdiag.Arm
	var receipt networkdiag.ArmReceipt
	armOK := false
	err = e.phase("arm", false, func(ctx context.Context, id string) (any, int, error) {
		r, ce := e.now()
		if ce != nil || !observerCurrent(obs[0], r) || !observerCurrent(obs[1], r) {
			return struct {
				Skipped bool `json:"skipped"`
			}{true}, 0, nil
		}
		arm = networkdiag.Arm{Version: 1, Kind: "ARM", Generation: p[1].Identity.Generation, Nonce: p[1].Launch.Nonce, OperationID: id, Candidate: p[1].Network.Binding, Control: p[0].Network.Binding, Gateway: p[1].Launch.Gateway, DurationMS: 30000, ControlProvenance: "host_backend_pinned_owner"}
		ce = e.worker(ctx, 1, "arm", p[1].Identity, func(r *worker.Request) { r.Arm = &arm }, &receipt)
		if ce == nil && !receipt.Matches(arm) {
			ce = ErrRefused
		}
		armOK = ce == nil
		return receipt, 1, nil
	})
	if err == nil && armOK {
		err = e.phase("connect", false, func(ctx context.Context, id string) (any, int, error) {
			r, ce := e.now()
			if ce != nil || !receipt.Current(networkdiag.ClockReading{WallNS: r.Wall, ContinuousNS: r.Continuous}) || !observerCurrent(obs[0], r) || !observerCurrent(obs[1], r) {
				return struct {
					Skipped bool `json:"skipped"`
				}{true}, 0, nil
			}
			q := worker.Request{Version: 1, Window: e.window, Identity: p[1].Identity, Peer: &base.Control}
			input, _ := json.Marshal(q)
			input = append(input, '\n')
			if e.executable(3) != nil {
				return nil, 0, ErrRefused
			}
			e.witness(contract.StaticFilePath(3), []string{"connect"})
			c, ce := callChild(ctx, contract.StaticFilePath(3), []string{"connect"}, input, 4096)
			var result sshx.N1ConnectResult
			if !c.closed {
				e.localDirty = true
			}
			if decode(c.raw, &result, 4096) == nil && result.Version == 1 && result.Binding.SessionID == p[1].Identity.Session && result.Binding.Generation == p[1].Identity.Generation && result.Binding.BackendObject == p[1].Identity.Backend && result.ControlIPv4 == base.Control.Address && result.Errno >= 0 && result.Errno <= 4095 && result.ElapsedUS >= 0 && result.ElapsedUS <= 10000000 && result.Connected == (result.Code == "connected") && (result.Code == "connected" || result.Code == "timeout" || result.Code == "socket_error") {
				after, afterErr := e.now()
				facts.ConnectValid = true
				facts.Connected = result.Connected
				facts.Timeout = result.Code == "timeout"
				facts.Errno = result.Errno
				facts.TimingOK = result.TimingOK && result.ElapsedUS <= 4000000 && result.CloseOK && afterErr == nil && connectEnvelope(obs, receipt, r, after)
				facts.TransportOK = ce == nil
			}
			// The fact remains even if the child/RPC transport later fails.
			return result, 1, nil
		})
	}
	after, ae := e.positives("positive-after")
	facts.PositiveAfter = after
	afterControls, ace := e.controls("network-controls-after")
	facts.ControlsAfter = afterControls
	both := true
	for i, o := range obs {
		var ok bool
		var oe error
		summaries[i], ok, oe = e.endObserver(o)
		if oe != nil {
			return oe
		}
		both = both && ok
	}
	joined = true
	facts.ObserversComplete = both
	if e.check(false) != nil {
		return ErrRefused
	}
	if err == nil && armOK {
		err = e.phase("collect", false, func(ctx context.Context, id string) (any, int, error) {
			var s networkdiag.Summary
			ce := e.worker(ctx, 1, "collect", p[1].Identity, func(r *worker.Request) { r.Operation = arm.OperationID }, &s)
			facts.WatchComplete = ce == nil && s.Complete && s.Matches(arm, receipt.Armed)
			facts.Provenance = facts.WatchComplete && distinct(p)
			facts.ExactARPPolicyDenied = s.Counters.VMIdentified[0] > 0 && s.Counters.VMOutcomes[0][0] == s.Counters.VMIdentified[0] && s.Counters.VMARPEvaluation[4] >= s.Counters.VMIdentified[0] && s.Counters.VMLeaseState[0][3] == s.Counters.VMIdentified[0] && s.Counters.VMTargetPredicates[0][0][1] == s.Counters.VMIdentified[0] && s.Counters.VMTargetPredicates[0][1][1] == s.Counters.VMIdentified[0] && s.Counters.VMWriteAttempts[0] == 0
			facts.HostWriteAttempted = s.Counters.VMWriteAttempts[0] > 0
			facts.VMWriteReturned = s.Counters.VMWriteAttempts[0] > 0 || s.Counters.VMWriteAttempts[1] > 0 || s.Counters.VMWriteAttempts[2] > 0
			return s, 1, nil
		})
	}
	facts.OutgoingSilent = summaries[1].NegativeInference.CandidatePreEmissionAbsenceIfDriverControlsPass
	// Future actual kernel/interface/offload capture qualification is unresolved.
	// Deterministic filter fixtures are never admitted as that live premise.
	facts.CaptureQualified = false
	if ae != nil || ace != nil || err != nil {
		return ErrRefused
	}
	return e.networkVerdict(facts)
}
func (e *engine) networkVerdict(facts adjudicate.NetworkEvidence) error {
	raw, err := contract.Encode(adjudicate.Network(facts))
	if err != nil {
		return err
	}
	return e.archive.write("network-verdict.json", raw)
}

func roleIndex(r string) int {
	if r == "control" {
		return 0
	}
	return 1
}

func connectEnvelope(obs [2]*observer, a networkdiag.ArmReceipt, start, end clock.Reading) bool {
	if end.Wall < start.Wall || end.Continuous < start.Continuous || !a.Current(networkdiag.ClockReading{WallNS: end.Wall, ContinuousNS: end.Continuous}) {
		return false
	}
	for _, o := range obs {
		if o == nil || end.Wall < o.started.Wall || end.Continuous < o.started.Continuous || end.Wall-o.started.Wall >= 30e9 || end.Continuous-o.started.Continuous >= 30e9 {
			return false
		}
	}
	return true
}
