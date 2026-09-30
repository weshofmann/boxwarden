//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package sessionruntime

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"net"
	"time"
)

type ownerNetworkDependencies struct {
	observe  func(context.Context, string) (tart.DiagnosticMACObservation, error)
	enrolled func() error
}

func (o *Owner) InspectDiagnosticNetwork(parent context.Context) (networkdiag.Inspection, error) {
	refuse := func() (networkdiag.Inspection, error) { return networkdiag.Inspection{}, networkdiag.ErrMetadata }
	if o == nil || parent.Err() != nil {
		return refuse()
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	before := o.Snapshot(ctx)
	if !readyImportSnapshot(before) || ctx.Err() != nil {
		return refuse()
	}
	o.mu.Lock()
	r := clipboardRuntime{connection: o.connection, binding: o.binding, stateRoot: o.stateRoot, sessionName: o.sessionName}
	exact := o.clipboardConnectionMatchesLocked(r.connection, r.binding)
	path, home, observer := o.tartPath, o.tartHome, o.observer
	observe := o.deps.network.observe
	enrolled := o.deps.network.enrolled
	o.mu.Unlock()
	if !exact || before.Binding != r.binding || admitClipboardRecord(r) != nil {
		return refuse()
	}
	if enrolled == nil {
		enrolled = func() error {
			current, err := config.LoadN1CurrentEnrollment()
			if err != nil {
				return err
			}
			selected, err := current.Domain(config.N1Domain)
			if err != nil {
				return err
			}
			host, err := current.HostAdmission()
			if err != nil || selected.StateRoot != r.stateRoot || r.binding.Domain != config.N1Domain || r.sessionName != config.N1EnrolledSessionName || host.Host.TartExecutable != path || host.Host.TartHome != home {
				return config.ErrN1Enrollment
			}
			return nil
		}
	}
	if enrolled() != nil {
		return refuse()
	}
	if observe == nil {
		typed, ok := observer.(interface {
			ObserveDiagnosticMAC(context.Context, string) (tart.DiagnosticMACObservation, error)
		})
		if !ok {
			return refuse()
		}
		observe = typed.ObserveDiagnosticMAC
	}
	mac, err := observe(ctx, r.binding.BackendObject)
	if err != nil {
		return refuse()
	}
	resolver := o.deps.address(path, home)
	if resolver == nil {
		return refuse()
	}
	address, err := resolver.Resolve(ctx, r.binding.BackendObject)
	ip := net.ParseIP(address).To4()
	if err != nil || ip == nil || address != r.connection.Address {
		return refuse()
	}
	result := networkdiag.Inspection{Binding: networkdiag.Binding{Domain: r.binding.Domain, SessionID: r.binding.SessionID, Generation: r.binding.Generation, BackendKind: r.binding.BackendKind, BackendObject: r.binding.BackendObject, Address: [4]uint8{ip[0], ip[1], ip[2], ip[3]}, MAC: mac.MAC}, PinFingerprint: r.connection.Pin.Fingerprint, ConfigSHA256: mac.ConfigSHA256, ObservedUnixNS: uint64(time.Now().UnixNano())}
	if !result.Valid() || !o.clipboardStillReady(ctx, r) || enrolled() != nil || ctx.Err() != nil {
		return refuse()
	}
	// The final Snapshot may probe a connection newly published by Ready.
	// Require that its retained authority still describes the captured pair.
	o.mu.Lock()
	exact = o.connection == r.connection &&
		o.clipboardConnectionMatchesLocked(r.connection, r.binding) &&
		o.stateRoot == r.stateRoot && o.sessionName == r.sessionName
	o.mu.Unlock()
	if !exact || ctx.Err() != nil {
		return refuse()
	}
	return result, nil
}
