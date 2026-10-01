//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/session"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDiagnosticOriginalPairNeverRebound(t *testing.T) {
	p := [2]networkdiag.Inspection{{Binding: networkdiag.Binding{Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000001", Generation: "00000000-0000-4000-8000-000000000011", BackendKind: "tart", BackendObject: "candidate", Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}, PinFingerprint: "SHA256:" + strings.Repeat("A", 43), ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: 1}}
	p[1] = p[0]
	p[1].Binding.SessionID = "00000000-0000-4000-8000-000000000002"
	p[1].Binding.Generation = "00000000-0000-4000-8000-000000000012"
	p[1].Binding.BackendObject = "control"
	p[1].Binding.Address[3] = 3
	p[1].Binding.MAC[5] = 3
	next := p
	next[0].ObservedUnixNS = 2
	next[1].ObservedUnixNS = 2
	if !sameDiagnosticPair(p, next) {
		t.Fatal("fresh same original tuple refused")
	}
	for _, kind := range []string{"pin", "config", "generation", "address", "mac", "backend"} {
		t.Run(kind, func(t *testing.T) {
			next := p
			switch kind {
			case "pin":
				next[1].PinFingerprint = "SHA256:" + strings.Repeat("B", 43)
			case "config":
				next[1].ConfigSHA256 = strings.Repeat("b", 64)
			case "generation":
				next[1].Binding.Generation = "00000000-0000-4000-8000-000000000013"
			case "address":
				next[1].Binding.Address[3] = 4
			case "mac":
				next[1].Binding.MAC[5] = 4
			case "backend":
				next[1].Binding.BackendObject = "new-control"
			}
			if sameDiagnosticPair(p, next) {
				t.Fatal("original pair drift silently rebound")
			}
		})
	}
}

// This composition supplies only synthetic current-pair observations; the
// retained handle contains the actual independently draining socket watch.
type diagnosticWatchFixtureHandle struct {
	*fakeHandle
	watch *networkdiag.Watch
}

func (h *diagnosticWatchFixtureHandle) DiagnosticWatch() *networkdiag.Watch { return h.watch }
func TestDiagnosticOwnerCollectRechecksOriginalPair(t *testing.T) {
	if _, e := networkdiag.NewLaunchClock(); e != nil {
		t.Skip("native suspend-aware clock unavailable; production refuses before spawn")
	}
	for _, drift := range []bool{false, true} {
		t.Run(map[bool]string{false: "stable", true: "peer_pin_drift"}[drift], func(t *testing.T) {
			a := networkdiag.Arm{Version: 1, Kind: "ARM", Generation: "00000000-0000-4000-8000-000000000011", Nonce: "00000000-0000-4000-8000-000000000030", OperationID: "00000000-0000-4000-8000-000000000040", DurationMS: 1000, Gateway: [4]uint8{192, 168, 64, 1}, ControlProvenance: "host_backend_pinned_owner"}
			a.Candidate = networkdiag.Binding{Domain: config.N1Domain, SessionID: "00000000-0000-4000-8000-000000000001", Generation: a.Generation, BackendKind: "tart", BackendObject: "candidate", Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}
			a.Control = a.Candidate
			a.Control.SessionID = "00000000-0000-4000-8000-000000000002"
			a.Control.Generation = "00000000-0000-4000-8000-000000000012"
			a.Control.BackendObject = "control"
			a.Control.Address[3] = 3
			a.Control.MAC[5] = 3
			syscall.ForkLock.RLock()
			fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
			if err == nil {
				for _, fd := range fds {
					syscall.CloseOnExec(fd)
					if e := syscall.SetNonblock(fd, true); e != nil {
						err = e
					}
				}
			}
			syscall.ForkLock.RUnlock()
			if err != nil {
				t.Fatal(err)
			}
			child := os.NewFile(uintptr(fds[1]), "synthetic-child")
			w := networkdiag.NewWatch(os.NewFile(uintptr(fds[0]), "synthetic-owner"), a.Generation, a.Nonce, a.Candidate.MAC)
			t.Cleanup(func() { w.Close(); child.Close() })
			child.SetDeadline(time.Now().Add(4 * time.Second))
			pair := [2]networkdiag.Inspection{{Binding: a.Candidate, PinFingerprint: "SHA256:" + strings.Repeat("A", 43), ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: 1}, {Binding: a.Control, PinFingerprint: "SHA256:" + strings.Repeat("B", 43), ConfigSHA256: strings.Repeat("b", 64), ObservedUnixNS: 1}}
			o := &Owner{handle: &diagnosticWatchFixtureHandle{watch: w}}
			calls := 0
			o.deps.diagnostic.pair = func(context.Context, networkdiag.Arm) ([2]networkdiag.Inspection, error) { calls++; return pair, nil }
			send := func(v any) error {
				raw, e := networkdiag.Frame(v)
				if e != nil {
					return e
				}
				n, e := child.Write(raw)
				if e == nil && n != len(raw) {
					return networkdiag.ErrMetadata
				}
				return e
			}
			if err = send(networkdiag.Hello{Version: 1, Kind: "HELLO", Generation: a.Generation, Nonce: a.Nonce, CandidateMAC: a.Candidate.MAC, Gateway: a.Gateway}); err != nil {
				t.Fatal(err)
			}
			if _, err = w.AwaitHello(t.Context()); err != nil {
				t.Fatal(err)
			}
			armed := networkdiag.Armed{Version: 1, Kind: "ARMED", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, ArmedOffsetNS: 100, CandidateLeaseValid: true}
			summary := networkdiag.Summary{Version: 1, Kind: "SUMMARY", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, ArmedOffsetNS: 100, EndOffsetNS: 1000000100, CandidateLeaseValid: true, CoverageScope: "identified_pair_headers", PacketCountUnobserved: true, Complete: true}
			worker := make(chan error, 1)
			go func() {
				raw, e := networkdiag.ReadFrame(child)
				var got networkdiag.Arm
				if e == nil {
					e = networkdiag.Decode(raw, &got)
				}
				if e == nil && got != a {
					e = networkdiag.ErrMetadata
				}
				if e == nil {
					e = send(armed)
				}
				if e == nil {
					time.Sleep(time.Second)
					e = send(summary)
				}
				worker <- e
			}()
			if _, err = o.ArmDiagnosticWatch(t.Context(), a); err != nil {
				t.Fatal(err)
			}
			if err = <-worker; err != nil {
				t.Fatal(err)
			}
			pair[0].ObservedUnixNS = 2
			pair[1].ObservedUnixNS = 2
			if drift {
				pair[1].PinFingerprint = "SHA256:" + strings.Repeat("C", 43)
			}
			got, err := o.CollectDiagnosticWatch(t.Context(), a.OperationID)
			if drift {
				if err == nil {
					t.Fatal("post-summary peer drift rebound original ARM")
				}
				if _, e := o.CollectDiagnosticWatch(t.Context(), a.OperationID); e == nil {
					t.Fatal("invalid watch revived")
				}
			} else if err != nil || got != summary {
				t.Fatalf("stable original pair: %v", err)
			}
			if calls != 2 {
				t.Fatalf("pair observations=%d", calls)
			}
			if _, err = o.ArmDiagnosticWatch(t.Context(), a); err == nil {
				t.Fatal("Owner rearmed consumed generation")
			}
		})
	}
}
func TestDiagnosticPeerRecordMustRemainExactRunningReady(t *testing.T) {
	b := networkdiag.Binding{Domain: config.N1Domain, SessionID: "00000000-0000-4000-8000-000000000001", Generation: "00000000-0000-4000-8000-000000000011", BackendKind: "tart", BackendObject: "control"}
	r := session.Record{Domain: config.N1Domain, Name: session.Name(config.N1ControlName), ID: b.SessionID, StartGeneration: b.Generation, Backend: session.BackendRef{Kind: b.BackendKind, ObjectID: b.BackendObject}, IntendedState: session.StateRunning, Readiness: session.ReadinessRecord{Status: session.ReadinessReady}}
	if !diagnosticRecordMatches(r, config.N1ControlName, b) {
		t.Fatal("exact record refused")
	}
	mutations := map[string]func(*session.Record){"domain": func(r *session.Record) { r.Domain = "other" }, "name": func(r *session.Record) { r.Name = "other" }, "session": func(r *session.Record) { r.ID = "other" }, "generation": func(r *session.Record) { r.StartGeneration = "other" }, "backend": func(r *session.Record) { r.Backend.Kind = "other" }, "object": func(r *session.Record) { r.Backend.ObjectID = "other" }, "intent": func(r *session.Record) { r.IntendedState = session.StateStopped }, "ready": func(r *session.Record) { r.Readiness.Status = session.ReadinessNotReady }}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := r
			mutate(&bad)
			if diagnosticRecordMatches(bad, config.N1ControlName, b) {
				t.Fatal("peer record drift admitted")
			}
		})
	}
}
