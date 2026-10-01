//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

var ErrRefused = errors.New("n1 worker refused or unknown")

type Identity struct {
	Session    string `json:"session"`
	Backend    string `json:"backend"`
	Generation string `json:"generation"`
}

func (i Identity) valid() bool {
	return contract.UUID(i.Session) && contract.UUID(i.Generation) && i.Backend != ""
}

type Reader interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
	InspectDiagnosticNetwork(context.Context, supervisor.Binding) (networkdiag.Inspection, error)
}
type Process struct {
	PID      uint32 `json:"pid"`
	BirthUS  uint64 `json:"birth_us"`
	UniqueID uint64 `json:"unique_id"`
}
type Launch struct {
	Nonce    string                   `json:"nonce"`
	Gateway  [4]uint8                 `json:"gateway"`
	Owner    Process                  `json:"owner"`
	Child    Process                  `json:"child"`
	Anchor   networkdiag.ClockReading `json:"anchor"`
	Deadline networkdiag.ClockReading `json:"deadline"`
	Observed networkdiag.ClockReading `json:"observed"`
}
type RuntimeObservation struct {
	Version     int                           `json:"version"`
	Role        string                        `json:"role"`
	Name        string                        `json:"name"`
	Identity    Identity                      `json:"identity"`
	Ready       supervisor.Snapshot           `json:"ready"`
	Network     networkdiag.Inspection        `json:"network"`
	Connection  sshx.Connection               `json:"connection"`
	Certificate sshx.N1CertificateObservation `json:"certificate"`
	PrivateKey  FileMetadata                  `json:"private_key"`
	HostPinSHA  string                        `json:"host_pin_sha"`
	CASHA       string                        `json:"ca_sha"`
	Launch      *Launch                       `json:"launch"`
}
type Worker struct {
	domain   config.Domain
	observer backend.Observer
	creator  backend.Creator
	reader   Reader
	guest    *sshx.Client
	static   fixed.StaticInputs
}

func Compose(observer backend.Observer, creator backend.Creator) (*Worker, error) {
	if observer == nil || creator == nil {
		return nil, ErrRefused
	}
	loaded, e := config.LoadN1CurrentEnrollment()
	if e != nil {
		return nil, ErrRefused
	}
	d, e := loaded.Domain(config.N1Domain)
	if e != nil || d.StateRoot != contract.StateRoot {
		return nil, ErrRefused
	}
	reader, e := supervisor.NewExactSnapshotReader(d.StateRoot + "/runtime")
	if e != nil {
		return nil, ErrRefused
	}
	s, e := fixed.ReadStatic()
	if e != nil {
		return nil, ErrRefused
	}
	return &Worker{domain: d, observer: observer, creator: creator, reader: reader, guest: sshx.NewClient(sshx.NewExecRunner()), static: s}, nil
}
func admitRecord(r session.Record, id Identity) error {
	if !id.valid() || r.Domain != config.N1Domain || string(r.Name) != roleName || r.ID != id.Session || r.Backend.Kind != "tart" || r.Backend.ObjectID != id.Backend || r.StartGeneration != id.Generation || r.Mode != session.ModeQuarantine || r.IntendedState != session.StateRunning || r.GoldenRevision != contract.BaseName || r.RecipeIntentDigest != "" {
		return ErrRefused
	}
	return nil
}
func binding(id Identity) supervisor.Binding {
	return supervisor.Binding{Domain: config.N1Domain, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend, Generation: id.Generation}
}
func guestBinding(id Identity) sshx.N1GuestBinding {
	return sshx.N1GuestBinding{Version: 1, Domain: config.N1Domain, SessionID: id.Session, BackendKind: "tart", BackendObject: id.Backend, Generation: id.Generation}
}
func ready(s supervisor.Snapshot, b supervisor.Binding) bool {
	return s.Binding == b && s.BackendRunning && s.SerialHealthy && s.PinPresent && s.CertificateCurrent && s.ProbeOK && s.ZoneMatches
}
func bounded(v any, limit int) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil || len(raw)+1 > limit {
		return nil, ErrRefused
	}
	return append(raw, '\n'), nil
}
