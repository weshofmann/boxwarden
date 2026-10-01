//go:build n1diagnostic && !n1candidate

package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

type launchObservationHandle struct {
	*fakeHandle
	watch *networkdiag.Watch
	child networkdiag.ProcessCorrelation
	bad   bool
}

func (h *launchObservationHandle) DiagnosticWatch() *networkdiag.Watch { return h.watch }
func (h *launchObservationHandle) RetainedDiagnosticProcess() (networkdiag.ProcessCorrelation, error) {
	if h.bad || !h.RetainedChildLive() {
		return networkdiag.ProcessCorrelation{}, networkdiag.ErrMetadata
	}
	return h.child, nil
}
func launchOwnerFixture(t *testing.T) (*fixture, *ownerClipboardClient, *launchObservationHandle) {
	t.Helper()
	if _, e := networkdiag.NewLaunchClock(); e != nil {
		t.Skip("native clock unavailable: production refuses before spawn")
	}
	f, c := readyClipboardFixture(t)
	o := f.owner
	o.deps.network.enrolled = func() error { return nil }
	f.record.Domain = "n1qualification"
	o.binding.Domain = "n1qualification"
	o.sshBinding.Domain = "n1qualification"
	o.connection.Binding = o.sshBinding
	o.expectedPin.Domain = "n1qualification"
	o.connection.Pin = o.expectedPin
	o.pins = &flakyPinStore{pin: o.expectedPin}
	o.connection.Address = "192.168.64.2"
	o.certificate.Identity = o.sshBinding.CertificateIdentity()
	o.certificate.Principal = o.sshBinding.Principal()
	if e := session.SaveRecord(f.root, "n1qualification", f.record); e != nil {
		t.Fatal(e)
	}
	mac := [6]uint8{2, 0, 0, 0, 0, 2}
	o.deps.network.observe = func(context.Context, string) (tart.DiagnosticMACObservation, error) {
		return tart.DiagnosticMACObservation{MAC: mac, ConfigSHA256: strings.Repeat("a", 64)}, nil
	}
	o.deps.address = func(string, string) backend.AddressResolver {
		return readyAddress(func(context.Context, string) (string, error) { return "192.168.64.2", nil })
	}
	syscall.ForkLock.RLock()
	fds, e := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if e == nil {
		for _, fd := range fds {
			syscall.CloseOnExec(fd)
			if err := syscall.SetNonblock(fd, true); err != nil {
				e = err
			}
		}
	}
	syscall.ForkLock.RUnlock()
	if e != nil {
		t.Fatal(e)
	}
	child := os.NewFile(uintptr(fds[1]), "synthetic-launch-child")
	watch := networkdiag.NewWatch(os.NewFile(uintptr(fds[0]), "synthetic-launch-owner"), o.binding.Generation, "00000000-0000-4000-8000-000000000099", mac)
	t.Cleanup(func() { watch.Close(); child.Close() })
	child.SetDeadline(time.Now().Add(time.Second))
	hello := networkdiag.Hello{Version: 1, Kind: "HELLO", Generation: o.binding.Generation, Nonce: "00000000-0000-4000-8000-000000000099", CandidateMAC: mac, Gateway: [4]uint8{192, 168, 64, 1}}
	raw, e := networkdiag.Frame(hello)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = child.Write(raw); e != nil {
		t.Fatal(e)
	}
	if _, e = watch.AwaitHello(t.Context()); e != nil {
		t.Fatal(e)
	}
	h := &launchObservationHandle{fakeHandle: f.handle, watch: watch, child: networkdiag.ProcessCorrelation{PID: 777, BirthUS: 9, UniqueID: 11}}
	o.handle = h
	o.deps.diagnostic.process = func() (networkdiag.ProcessCorrelation, error) {
		return networkdiag.ProcessCorrelation{PID: 888, BirthUS: 12, UniqueID: 13}, nil
	}
	return f, c, h
}
func TestDiagnosticLaunchObservationRetainsCompleteAuthority(t *testing.T) {
	for _, mode := range []string{"stable", "renewal", "address", "port", "identity", "pin", "generation", "runtime", "handle", "watch", "birth", "unique", "unavailable", "ownerbirth"} {
		t.Run(mode, func(t *testing.T) {
			f, c, h := launchOwnerFixture(t)
			o := f.owner
			before, e := h.watch.Observe()
			if e != nil {
				t.Fatal(e)
			}
			observe := o.deps.network.observe
			called := false
			if mode == "ownerbirth" {
				calls := 0
				o.deps.diagnostic.process = func() (networkdiag.ProcessCorrelation, error) {
					calls++
					b := uint64(12)
					if calls > 1 {
						b = 14
					}
					return networkdiag.ProcessCorrelation{PID: 888, BirthUS: b, UniqueID: 13}, nil
				}
			}
			c.probe = func(sshx.Connection) error { return nil }
			o.deps.network.observe = func(ctx context.Context, id string) (tart.DiagnosticMACObservation, error) {
				called = true
				if mode == "renewal" {
					if e := o.Ready(ctx); e != nil {
						t.Fatal(e)
					}
				}
				o.mu.Lock()
				switch mode {
				case "address":
					o.connection.Address = "192.168.64.3"
				case "port":
					o.connection.Port = 23
				case "identity":
					o.connection.IdentityFile += "-changed"
				case "pin":
					o.expectedPin.Fingerprint = "SHA256:" + strings.Repeat("B", 43)
					o.connection.Pin = o.expectedPin
					o.pins = &flakyPinStore{pin: o.expectedPin}
				case "generation":
					o.binding.Generation = "00000000-0000-4000-8000-000000000098"
				case "runtime":
					o.runtimePath += "-changed"
				case "handle":
					replacement := *h
					o.handle = &replacement
				case "watch":
					h.watch = nil
				case "birth":
					h.child.BirthUS++
				case "unique":
					h.child.UniqueID++
				case "unavailable":
					h.bad = true
				case "ownerbirth":
					o.deps.diagnostic.process = func() (networkdiag.ProcessCorrelation, error) {
						return networkdiag.ProcessCorrelation{PID: 888, BirthUS: 14, UniqueID: 13}, nil
					}
				}
				o.mu.Unlock()
				return observe(ctx, id)
			}
			got, e := o.ObserveDiagnosticLaunch(t.Context())
			valid := mode == "stable" || mode == "renewal"
			if valid {
				if e != nil || !got.Valid() || got.Watch.Anchor != before.Anchor || got.Watch.Deadline != before.Deadline {
					t.Fatal("same authority refused or extended", e)
				}
				again, e := o.ObserveDiagnosticLaunch(t.Context())
				if e != nil || again.Watch.Deadline != before.Deadline {
					t.Fatal("repeated observation extended/refused", e)
				}
			} else if e == nil {
				t.Fatal("captured authority replacement admitted", mode)
			}
			if !called {
				t.Fatal("did not reach real MAC/inspection composition")
			}
		})
	}
}
func TestDiagnosticLaunchObservationUnavailableCannotArm(t *testing.T) {
	if _, e := (&Owner{}).ObserveDiagnosticLaunch(t.Context()); e == nil {
		t.Fatal("missing retained watch admitted")
	}
	o := &Owner{}
	if _, e := o.ArmDiagnosticWatchReceipt(t.Context(), networkdiag.Arm{}); e == nil {
		t.Fatal("missing observation manufactured ARM authority")
	}
}
