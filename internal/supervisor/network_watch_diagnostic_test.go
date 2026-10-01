//go:build n1diagnostic && !n1candidate

package supervisor

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"math"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

type watchControlOwner struct {
	*networkTestOwner
	arm                    networkdiag.Arm
	armed                  networkdiag.Armed
	summary                networkdiag.Summary
	armCalls, collectCalls int
}

func (o *watchControlOwner) ArmDiagnosticWatch(_ context.Context, a networkdiag.Arm) (networkdiag.Armed, error) {
	o.armCalls++
	if a != o.arm {
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	return o.armed, nil
}
func (o *watchControlOwner) CollectDiagnosticWatch(_ context.Context, id string) (networkdiag.Summary, error) {
	o.collectCalls++
	if id != o.arm.OperationID {
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	return o.summary, nil
}
func watchControlFixture(t *testing.T) (Binding, *watchControlOwner) {
	t.Helper()
	b := minimalRequest(t).Binding
	b.Domain = "n1qualification"
	b.SessionID = "00000000-0000-4000-8000-000000000001"
	b.Generation = "00000000-0000-4000-8000-000000000011"
	candidate := networkdiag.Binding{Domain: b.Domain, SessionID: b.SessionID, Generation: b.Generation, BackendKind: b.BackendKind, BackendObject: b.BackendObject, Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}
	control := candidate
	control.SessionID = "00000000-0000-4000-8000-000000000002"
	control.Generation = "00000000-0000-4000-8000-000000000012"
	control.BackendObject = "control"
	control.Address[3] = 3
	control.MAC[5] = 3
	a := networkdiag.Arm{Version: 1, Kind: "ARM", Generation: b.Generation, Nonce: "00000000-0000-4000-8000-000000000030", OperationID: "00000000-0000-4000-8000-000000000040", Candidate: candidate, Control: control, Gateway: [4]uint8{192, 168, 64, 1}, DurationMS: 1000, ControlProvenance: "host_backend_pinned_owner"}
	armed := networkdiag.Armed{Version: 1, Kind: "ARMED", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, ArmedOffsetNS: 100, CandidateLeaseValid: true}
	summary := networkdiag.Summary{Version: 1, Kind: "SUMMARY", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, ArmedOffsetNS: 100, EndOffsetNS: 1000000100, CandidateLeaseValid: true, CoverageScope: "identified_pair_headers", PacketCountUnobserved: true, Complete: true}
	return b, &watchControlOwner{networkTestOwner: &networkTestOwner{clipboardTestOwner: &clipboardTestOwner{binding: b, ready: true}}, arm: a, armed: armed, summary: summary}
}
func TestDiagnosticWatchPrivateExactDispatch(t *testing.T) {
	for _, action := range []string{"n1_network_arm", "n1_network_collect"} {
		for _, kind := range []string{"valid", "extra", "binding", "expired", "invalid_result"} {
			t.Run(action+"/"+kind, func(t *testing.T) {
				b, o := watchControlFixture(t)
				deadline := uint64(time.Now().Add(time.Second).UnixNano())
				requestBinding := b
				if kind == "binding" {
					requestBinding.Generation = "00000000-0000-4000-8000-000000000099"
				}
				if kind == "expired" {
					deadline = 1
				}
				var request any
				if action == "n1_network_arm" {
					request = networkArmRequest{1, action, requestBinding, deadline, o.arm}
					if kind == "invalid_result" {
						o.armed.CandidateLeaseValid = false
					}
				} else {
					request = networkCollectRequest{1, action, requestBinding, deadline, o.arm.OperationID}
					if kind == "invalid_result" {
						o.summary.Counters.VMDispatchCompleted = 1
					}
				}
				raw, _ := json.Marshal(request)
				if kind == "extra" {
					raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"root":"/arbitrary"`, 1))
				}
				server, client := net.Pipe()
				done := make(chan struct{})
				go func() { handleControl(t.Context(), server, b, o, func() error { return nil }); close(done) }()
				client.SetDeadline(time.Now().Add(2 * time.Second))
				if err := writeFrame(client, raw); err != nil {
					t.Fatal(err)
				}
				out, err := networkdiag.ReadFrame(client)
				client.Close()
				<-done
				calls := o.armCalls + o.collectCalls
				if kind == "valid" || kind == "invalid_result" {
					var ok bool
					if action == "n1_network_arm" {
						var result networkArmResponse
						if networkdiag.Decode(out, &result) != nil {
							t.Fatalf("response decode: %v", err)
						}
						ok = result.OK
					} else {
						var result networkCollectResponse
						if networkdiag.Decode(out, &result) != nil {
							t.Fatalf("response decode: %v", err)
						}
						ok = result.OK
					}
					if err != nil || calls != 1 || ok != (kind == "valid") {
						t.Fatalf("response ok=%v calls=%d err=%v", ok, calls, err)
					}
				} else if err == nil || calls != 0 {
					t.Fatalf("invalid dispatch returned response or reached Owner: %v calls=%d", err, calls)
				}
			})
		}
	}
}
func TestDiagnosticSummaryControlEnvelopeFitsFrame(t *testing.T) {
	b, o := watchControlFixture(t)
	s := o.summary
	s.Candidate.BackendObject = strings.Repeat("x", 128)
	s.Control.BackendObject = strings.Repeat("y", 128)
	s.Candidate.Address = [4]uint8{192, 168, 255, 254}
	s.Control.Address = [4]uint8{192, 168, 255, 253}
	s.Gateway = [4]uint8{192, 168, 255, 252}
	s.Candidate.MAC = [6]uint8{254, 255, 255, 255, 255, 254}
	s.Control.MAC = [6]uint8{254, 255, 255, 255, 255, 253}
	s.DurationMS = 30000
	s.ArmedOffsetNS = math.MaxUint64
	s.EndOffsetNS = math.MaxUint64
	s.CandidateLeaseValid = false
	s.Complete = false
	var fill func(reflect.Value)
	fill = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Struct, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				fill(v.Index(i))
			}
		case reflect.Uint16:
			v.SetUint(4096)
		case reflect.Uint64:
			v.SetUint(math.MaxUint64)
		}
	}
	// Struct and array traversal are kept separate because reflect.Len rejects structs.
	fill = func(v reflect.Value) {
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				fill(v.Field(i))
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				fill(v.Index(i))
			}
		case reflect.Uint16:
			v.SetUint(4096)
		case reflect.Uint64:
			v.SetUint(math.MaxUint64)
		}
	}
	fill(reflect.ValueOf(&s.Counters).Elem())
	b.BackendObject = s.Candidate.BackendObject
	raw, err := networkdiag.Encode(networkCollectResponse{Version: 1, Binding: b, OK: true, Summary: s})
	if err != nil || len(raw) > 4096 {
		t.Fatalf("finite summary envelope=%d: %v", len(raw), err)
	}
	t.Logf("conservative maximum-width control response=%d bytes", len(raw))
}

func TestDiagnosticPrivateCollectCompleteCounterImplications(t *testing.T) {
	for _, mode := range []string{"vm_refresh_failure", "host_refresh_failure", "unidentified_refresh_failure", "vm_length_mismatch", "host_length_mismatch", "valid_vm_denial", "valid_host_denial", "valid_vm_api_full", "valid_host_api_full"} {
		t.Run(mode, func(t *testing.T) {
			b, o := watchControlFixture(t)
			c := &o.summary.Counters
			switch mode {
			case "unidentified_refresh_failure":
				c.VMDispatchStarted = 1
				c.VMDispatchCompleted = 1
				c.RefreshResults[0][2] = 1
			case "vm_refresh_failure", "vm_length_mismatch", "valid_vm_denial", "valid_vm_api_full":
				c.VMDispatchStarted = 1
				c.VMDispatchCompleted = 1
				c.VMIdentified[0] = 1
				c.VMLeaseState[0][0] = 1
				c.VMTargetPredicates[0][0][0] = 1
				c.VMTargetPredicates[0][1][0] = 1
				if mode == "vm_refresh_failure" {
					c.RefreshResults[0][2] = 1
					c.VMOutcomes[0][2] = 1
				} else {
					c.RefreshResults[0][1] = 1
					if mode == "valid_vm_denial" {
						c.VMOutcomes[0][0] = 1
					} else {
						c.VMWriteAttempts[0] = 1
						c.VMWriteBytes[0] = [2]uint64{42, 42}
						c.VMOutcomes[0][3] = 1
						if mode == "vm_length_mismatch" {
							c.VMWriteBytes[0][1] = 41
							c.VMOutcomes[0][3] = 0
							c.VMOutcomes[0][4] = 1
						}
					}
				}
			case "host_refresh_failure", "host_length_mismatch", "valid_host_denial", "valid_host_api_full":
				c.HostDispatchStarted = 1
				c.HostDispatchCompleted = 1
				c.HostReplyClass[0] = 1
				if mode == "host_refresh_failure" {
					c.RefreshResults[1][2] = 1
					c.HostClassOutcomes[0][1] = 1
				} else {
					c.RefreshResults[1][1] = 1
					if mode == "valid_host_denial" {
						c.HostClassOutcomes[0][0] = 1
					} else {
						c.HostWriteAttempts[0] = 1
						c.HostWriteBytes[0] = [2]uint64{42, 42}
						c.HostClassOutcomes[0][2] = 1
						if mode == "host_length_mismatch" {
							c.HostWriteBytes[0][1] = 41
							c.HostClassOutcomes[0][2] = 0
							c.HostClassOutcomes[0][3] = 1
						}
					}
				}
			}
			raw, err := networkdiag.Encode(o.summary)
			var decoded networkdiag.Summary
			if err != nil || networkdiag.Decode(raw, &decoded) != nil || decoded != o.summary || !decoded.Counters.Arithmetic() {
				t.Fatal("private fixture is not strict balanced metadata")
			}
			request := networkCollectRequest{1, "n1_network_collect", b, uint64(time.Now().Add(time.Second).UnixNano()), o.arm.OperationID}
			raw, err = json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			server, client := net.Pipe()
			done := make(chan struct{})
			go func() { handleControl(t.Context(), server, b, o, func() error { return nil }); close(done) }()
			client.SetDeadline(time.Now().Add(2 * time.Second))
			if err = writeFrame(client, raw); err != nil {
				t.Fatal(err)
			}
			out, err := networkdiag.ReadFrame(client)
			client.Close()
			<-done
			var response networkCollectResponse
			valid := strings.HasPrefix(mode, "valid_")
			if err != nil || networkdiag.Decode(out, &response) != nil || o.collectCalls != 1 || response.OK != valid {
				t.Fatalf("private response OK=%v calls=%d err=%v", response.OK, o.collectCalls, err)
			}
			if valid && response.Summary != o.summary {
				t.Fatal("valid summary changed in transport")
			}
		})
	}
}

type legacyArmOwner struct {
	RuntimeOwner
	call func(context.Context, networkdiag.Arm) (networkdiag.Armed, error)
}

func (o legacyArmOwner) ArmDiagnosticWatch(ctx context.Context, a networkdiag.Arm) (networkdiag.Armed, error) {
	return o.call(ctx, a)
}
func TestDiagnosticARMRequiresActualHostSendReceipt(t *testing.T) {
	b, o := watchControlFixture(t)
	legacy := legacyArmOwner{RuntimeOwner: o, call: o.ArmDiagnosticWatch}
	req := networkArmRequest{1, "n1_network_arm", b, uint64(time.Now().Add(time.Second).UnixNano()), o.arm}
	raw, _ := json.Marshal(req)
	server, client := net.Pipe()
	done := make(chan struct{})
	go func() { handleControl(t.Context(), server, b, legacy, func() error { return nil }); close(done) }()
	client.SetDeadline(time.Now().Add(time.Second))
	writeFrame(client, raw)
	out, err := networkdiag.ReadFrame(client)
	client.Close()
	<-done
	var response networkArmResponse
	if err != nil || networkdiag.Decode(out, &response) != nil {
		t.Fatal(err)
	}
	if response.OK || o.armCalls != 0 {
		t.Fatal("ARM reached a legacy owner without retained host-send deadline")
	}
}

func (o *watchControlOwner) ArmDiagnosticWatchReceipt(ctx context.Context, a networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	armed, e := o.ArmDiagnosticWatch(ctx, a)
	sent, ce := networkdiag.HostClockNow()
	if ce != nil {
		sent = networkdiag.ClockReading{WallNS: uint64(time.Now().UnixNano()), ContinuousNS: 1}
	}
	d := uint64(a.DurationMS)*1000000 + 100000000
	return networkdiag.ArmReceipt{Version: 1, Armed: armed, Sent: sent, Deadline: networkdiag.ClockReading{WallNS: sent.WallNS + d, ContinuousNS: sent.ContinuousNS + d}}, e
}
