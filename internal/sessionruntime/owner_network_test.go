//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package sessionruntime

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"strings"
	"testing"
)

func TestDiagnosticNetworkInspectionRetainsPinAndRechecks(t *testing.T) {
	for _, kind := range []string{"valid", "address", "postprobe", "mac"} {
		t.Run(kind, func(t *testing.T) {
			f, c := readyClipboardFixture(t)
			f.owner.deps.network.enrolled = func() error { return nil }
			calls := 0
			// Synthetic retained authority is rebound only inside this fixture. The
			// production implementation has no rebind operation.
			f.record.Domain = "n1qualification"
			f.owner.binding.Domain = "n1qualification"
			f.owner.sshBinding.Domain = "n1qualification"
			f.owner.connection.Binding = f.owner.sshBinding
			f.owner.expectedPin.Domain = "n1qualification"
			f.owner.connection.Pin = f.owner.expectedPin
			f.owner.pins = &flakyPinStore{pin: f.owner.expectedPin}
			f.owner.connection.Address = "192.168.64.2"
			f.owner.certificate.Identity = f.owner.sshBinding.CertificateIdentity()
			f.owner.certificate.Principal = f.owner.sshBinding.Principal()
			if err := session.SaveRecord(f.root, "n1qualification", f.record); err != nil {
				t.Fatal(err)
			}
			f.owner.deps.network.observe = func(context.Context, string) (tart.DiagnosticMACObservation, error) {
				calls++
				if kind == "postprobe" {
					c.probe = func(sshx.Connection) error { return errors.New("lost proof") }
				}
				mac := [6]uint8{2, 0, 0, 0, 0, 1}
				if kind == "mac" {
					mac[0] = 1
				}
				return tart.DiagnosticMACObservation{MAC: mac, ConfigSHA256: strings.Repeat("a", 64)}, nil
			}
			f.owner.deps.address = func(string, string) backend.AddressResolver {
				return readyAddress(func(context.Context, string) (string, error) {
					if kind == "address" {
						return "192.168.64.3", nil
					}
					return "192.168.64.2", nil
				})
			}
			got, err := f.owner.InspectDiagnosticNetwork(t.Context())
			if kind == "valid" {
				if err != nil || got.Binding.Address != [4]uint8{192, 168, 64, 2} || got.PinFingerprint != ownerTestFingerprint() || got.ConfigSHA256 != strings.Repeat("a", 64) {
					t.Fatalf("inspection=%+v %v", got, err)
				}
			} else if err == nil {
				t.Fatal("drift admitted")
			}
			if calls != 1 {
				t.Fatalf("MAC observations=%d", calls)
			}
			if f.owner.pins.(*flakyPinStore).admitCalls != 0 {
				t.Fatal("inspection mutated pin")
			}
		})
	}
}

func TestDiagnosticNetworkInspectionCapturedConnectionAfterReady(t *testing.T) {
	for _, mode := range []string{"stable", "actual_Ready_address_replacement", "same_connection_renewal"} {
		t.Run(mode, func(t *testing.T) {
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
			if err := session.SaveRecord(f.root, "n1qualification", f.record); err != nil {
				t.Fatal(err)
			}
			o.deps.network.observe = func(context.Context, string) (tart.DiagnosticMACObservation, error) {
				return tart.DiagnosticMACObservation{MAC: [6]uint8{2, 0, 0, 0, 0, 2}, ConfigSHA256: strings.Repeat("a", 64)}, nil
			}
			captured, certificate := o.connection, o.certificate
			calls := 0
			var finalProbe sshx.Connection
			c.probe = func(conn sshx.Connection) error { finalProbe = conn; return nil }
			o.deps.address = func(string, string) backend.AddressResolver {
				return readyAddress(func(ctx context.Context, _ string) (string, error) {
					calls++
					call := calls
					if mode != "stable" && call == 1 {
						if err := o.Ready(ctx); err != nil {
							t.Fatal("actual Ready publication", err)
						}
					}
					if mode == "actual_Ready_address_replacement" && call > 1 {
						return "192.168.64.3", nil
					}
					return "192.168.64.2", nil
				})
			}
			got, err := o.InspectDiagnosticNetwork(t.Context())
			t.Logf("mode=%s returned=%v retained=%s final_probe=%s resolver_calls=%d err=%v", mode, got.Binding.Address, o.connection.Address, finalProbe.Address, calls, err)
			if mode == "actual_Ready_address_replacement" {
				if calls != 2 || o.connection.Address != "192.168.64.3" || finalProbe != o.connection {
					t.Fatal("actual Ready did not replace and verify new connection")
				}
				if err == nil {
					t.Fatal("captured old connection accepted after actual Ready replacement")
				}
			} else {
				if err != nil || got.Binding.Address != [4]uint8{192, 168, 64, 2} || o.connection != captured || finalProbe != captured {
					t.Fatalf("stable captured connection refused: %v", err)
				}
				if mode == "same_connection_renewal" && (calls != 2 || o.certificate == certificate) {
					t.Fatal("actual Ready did not renew certificate with the same retained connection")
				}
			}
			if o.pins.(*flakyPinStore).admitCalls != 0 {
				t.Fatal("network inspection admitted a pin")
			}
		})
	}
}
func TestDiagnosticNetworkInspectionCapturedFullConnectionRefusesReplacement(t *testing.T) {
	for _, mode := range []string{"port", "identity", "public_pin"} {
		t.Run(mode, func(t *testing.T) {
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
			if err := session.SaveRecord(f.root, "n1qualification", f.record); err != nil {
				t.Fatal(err)
			}
			var finalProbe sshx.Connection
			c.probe = func(conn sshx.Connection) error { finalProbe = conn; return nil }
			o.deps.network.observe = func(context.Context, string) (tart.DiagnosticMACObservation, error) {
				o.mu.Lock()
				switch mode {
				case "port":
					o.connection.Port = 23
				case "identity":
					o.connection.IdentityFile += "-replacement"
				case "public_pin":
					o.expectedPin.Fingerprint = "SHA256:" + strings.Repeat("B", 43)
					o.connection.Pin = o.expectedPin
					o.pins = &flakyPinStore{pin: o.expectedPin}
				}
				o.mu.Unlock()
				return tart.DiagnosticMACObservation{MAC: [6]uint8{2, 0, 0, 0, 0, 2}, ConfigSHA256: strings.Repeat("a", 64)}, nil
			}
			o.deps.address = func(string, string) backend.AddressResolver {
				return readyAddress(func(context.Context, string) (string, error) { return "192.168.64.2", nil })
			}
			if _, err := o.InspectDiagnosticNetwork(t.Context()); err == nil {
				t.Fatal("captured connection field replacement admitted")
			}
			if finalProbe != o.connection {
				t.Fatal("final Snapshot did not validate replaced retained connection")
			}
		})
	}
}
