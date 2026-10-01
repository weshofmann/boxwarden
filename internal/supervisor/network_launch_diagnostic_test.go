//go:build n1diagnostic && !n1candidate

package supervisor

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type launchRuntimeOwner struct {
	runtimeFixture
	a              networkdiag.Arm
	mu             sync.Mutex
	sent           networkdiag.ArmReceipt
	delay          bool
	observes, arms atomic.Int32
}

func (o *launchRuntimeOwner) ObserveDiagnosticLaunch(context.Context) (networkdiag.LaunchObservation, error) {
	o.observes.Add(1)
	r, e := networkdiag.HostClockNow()
	if e != nil {
		return networkdiag.LaunchObservation{}, e
	}
	anchor := networkdiag.ClockReading{WallNS: r.WallNS - uint64(31*time.Second), ContinuousNS: r.ContinuousNS - uint64(31*time.Second)}
	limit := networkdiag.ClockReading{WallNS: anchor.WallNS + uint64(networkdiag.PrearmCap), ContinuousNS: anchor.ContinuousNS + uint64(networkdiag.PrearmCap)}
	return networkdiag.LaunchObservation{Version: 1, Inspection: networkdiag.Inspection{Binding: o.a.Candidate, PinFingerprint: "SHA256:" + strings.Repeat("A", 43), ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: r.WallNS}, Watch: networkdiag.WatchObservation{Hello: networkdiag.Hello{Version: 1, Kind: "HELLO", Generation: o.a.Generation, Nonce: o.a.Nonce, CandidateMAC: o.a.Candidate.MAC, Gateway: o.a.Gateway}, Phase: "available", Anchor: anchor, Deadline: limit, Observed: r}, Owner: networkdiag.ProcessCorrelation{PID: 888, BirthUS: 1, UniqueID: 2}, Child: networkdiag.ProcessCorrelation{PID: 777, BirthUS: 3, UniqueID: 4}}, nil
}
func (o *launchRuntimeOwner) ArmDiagnosticWatchReceipt(_ context.Context, a networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	o.arms.Add(1)
	if a != o.a {
		return networkdiag.ArmReceipt{}, networkdiag.ErrMetadata
	}
	now, e := networkdiag.HostClockNow()
	if e != nil {
		return networkdiag.ArmReceipt{}, e
	}
	d := uint64(a.DurationMS)*1000000 + 100000000
	receipt := networkdiag.ArmReceipt{Version: 1, Armed: networkdiag.Armed{Version: 1, Kind: "ARMED", Generation: a.Generation, Nonce: a.Nonce, OperationID: a.OperationID, Candidate: a.Candidate, Control: a.Control, Gateway: a.Gateway, DurationMS: a.DurationMS, ControlProvenance: a.ControlProvenance, CandidateLeaseValid: true}, Sent: now, Deadline: networkdiag.ClockReading{WallNS: now.WallNS + d, ContinuousNS: now.ContinuousNS + d}}
	o.mu.Lock()
	o.sent = receipt
	o.mu.Unlock()
	if o.delay {
		time.Sleep(1250 * time.Millisecond)
	}
	return receipt, nil
}
func TestExactDiagnosticLaunchRPCAndActualSendReceipt(t *testing.T) {
	if _, e := networkdiag.HostClockNow(); e != nil {
		t.Skip("native clock unavailable; client refuses host clock admission")
	}
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "timely", true: "delayed_RPC"}[late], func(t *testing.T) {
			launch := minimalRequest(t)
			b, watch := watchControlFixture(t)
			root := filepath.Dir(filepath.Dir(filepath.Dir(launch.RuntimeDirectory)))
			launch.Binding = b
			launch.RuntimeDirectory = filepath.Join(root, b.Domain, b.SessionID, b.Generation)
			path, _, e := publishOrAdmitRequest(launch)
			if e != nil {
				t.Fatal(e)
			}
			o := &launchRuntimeOwner{runtimeFixture: runtimeFixture{done: make(chan struct{})}, a: watch.arm, delay: late}
			done := make(chan error, 1)
			go func() { done <- Run(context.Background(), path, o) }()
			client := &Client{RuntimeDirectory: launch.RuntimeDirectory}
			if _, e = awaitSnapshot(t.Context(), b, startupPolicy{timeout: time.Second, interval: time.Millisecond}, client.Snapshot); e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() {
				client.Stop(context.Background(), b)
				if e := <-done; e != nil {
					t.Error(e)
				}
			})
			reject := &rejectLauncher{}
			controller, e := NewExactController(root, reject)
			if e != nil {
				t.Fatal(e)
			}
			observation, e := controller.ObserveDiagnosticLaunch(t.Context(), b)
			if e != nil || !observation.Valid() || o.arms.Load() != 0 {
				t.Fatal("read-only exact controller observation", e)
			}
			reader, e := NewExactSnapshotReader(root)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = reader.ObserveDiagnosticLaunch(t.Context(), b); e != nil || o.arms.Load() != 0 || o.observes.Load() != 2 {
				t.Fatal("read-only exact reader observation", e)
			}
			got, e := controller.ArmDiagnosticWatchReceipt(t.Context(), b, o.a)
			o.mu.Lock()
			actual := o.sent
			o.mu.Unlock()
			if reject.calls.Load() != 0 {
				t.Fatal("read-only/current-generation RPC launched a child")
			}
			if o.arms.Load() != 1 {
				t.Fatal("ARM dispatch count")
			}
			if late {
				if e == nil {
					t.Fatal("RPC return restarted expired original send deadline")
				}
				now, _ := networkdiag.HostClockNow()
				if actual.Current(now) {
					t.Fatal("delayed fixture not actually expired")
				}
			} else if e != nil || got != actual || !got.Matches(o.a) {
				t.Fatal("actual send deadline replaced in typed transport", e)
			}
		})
	}
}
func TestLaunchObserveControlRejectsMalformedOrUnavailable(t *testing.T) {
	b, o := watchControlFixture(t)
	for _, mode := range []string{"unavailable", "extra", "duplicate", "expired", "binding"} {
		t.Run(mode, func(t *testing.T) {
			r := networkObserveRequest{Version: 1, Action: "n1_network_observe", Binding: b, ExpiresUnixNS: uint64(time.Now().Add(time.Second).UnixNano())}
			if mode == "expired" {
				r.ExpiresUnixNS = 1
			}
			if mode == "binding" {
				r.Binding.Generation = "00000000-0000-4000-8000-000000000098"
			}
			raw, _ := networkdiag.Encode(r)
			if mode == "extra" {
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"extra":1`, 1))
			}
			if mode == "duplicate" {
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1))
			}
			server, client := net.Pipe()
			done := make(chan struct{})
			go func() { handleControl(t.Context(), server, b, o, func() error { return nil }); close(done) }()
			client.SetDeadline(time.Now().Add(time.Second))
			writeFrame(client, raw)
			out, e := networkdiag.ReadFrame(client)
			client.Close()
			<-done
			if mode == "unavailable" {
				var response networkObserveResponse
				if e != nil || networkdiag.Decode(out, &response) != nil || response.OK {
					t.Fatal("unavailable owner manufactured observation", e)
				}
			} else if e == nil {
				t.Fatal("malformed request admitted")
			}
			if o.armCalls != 0 {
				t.Fatal("observation dispatched ARM")
			}
		})
	}
}
