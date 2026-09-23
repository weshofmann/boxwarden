// Package alphaqual qualifies a generic base on a newly created disposable
// clone. It has no production composition until a trusted guest acceptance
// inspector is available; all dependencies are explicit and injected.
package alphaqual

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/privateacl"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

const (
	maxEvidenceBytes = 8 << 20
	maxBOMBytes      = 1 << 20
	maxReadyAge      = 90 * time.Second
)

// RevisionRegistrar admits the exact stopped candidate without changing a
// domain-wide current pointer. golden.RegisterRevision can back an adapter.
type RevisionRegistrar interface {
	RegisterRevision(context.Context, string) (golden.Record, error)
}

// Lifecycle is the public session API. CreateFreshFromRevision is create-only
// under the session lock; an idempotent CreateFromRevision cannot prove a new
// qualification clone.
type Lifecycle interface {
	CreateFreshFromRevision(context.Context, string, session.Mode, string) (session.FreshCreation, error)
	Start(context.Context, string) (session.Record, error)
	Stop(context.Context, string) (session.Record, error)
}

type SnapshotReader interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type InspectionRequest struct {
	Session  session.Record
	Snapshot supervisor.Snapshot
}

// Inspection is a bounded guest acceptance result. The inspector must be a
// trusted host implementation of the guest acceptance matrix; a guest's own
// PASS text is not an implementation of this interface.
type Inspection struct {
	Binding    supervisor.Binding `json:"binding"`
	ObservedAt time.Time          `json:"observed_at"`
	Checks     []Check            `json:"checks"`
	BOM        json.RawMessage    `json:"bom"`
}

type GuestInspector interface {
	Inspect(context.Context, InspectionRequest) (Inspection, error)
}

type Dependencies struct {
	Domain    domain.ID
	Registrar RevisionRegistrar
	Lifecycle Lifecycle
	Observer  backend.Observer
	Snapshots SnapshotReader
	Inspector GuestInspector
	NewName   func() (string, error)
	Now       func() time.Time
	ACL       privateacl.Inspector
}

type Qualifier struct{ deps Dependencies }

// New requires every authority, including the guest inspector, explicitly.
// No production constructor silently substitutes a guest or host probe.
func New(deps Dependencies) (*Qualifier, error) {
	if _, err := domain.Parse(string(deps.Domain)); err != nil {
		return nil, fmt.Errorf("invalid qualification domain: %w", err)
	}
	if deps.Registrar == nil || deps.Lifecycle == nil || deps.Observer == nil || deps.Snapshots == nil || deps.Inspector == nil || deps.ACL == nil {
		return nil, errors.New("qualification requires registrar, lifecycle, observer, snapshot reader, guest inspector, and ACL inspector")
	}
	if deps.NewName == nil {
		deps.NewName = randomName
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Qualifier{deps: deps}, nil
}

type evidence struct {
	Version         int        `json:"version"`
	CandidateID     string     `json:"candidate_id"`
	PreparationKey  string     `json:"preparation_key"`
	CloneID         string     `json:"clone_id,omitempty"`
	SessionID       string     `json:"session_id,omitempty"`
	Generation      string     `json:"generation,omitempty"`
	ReadyObserved   *time.Time `json:"ready_observed_at,omitempty"`
	InspectObserved *time.Time `json:"inspection_observed_at,omitempty"`
	StoppedAt       *time.Time `json:"stopped_at,omitempty"`
	StopProven      bool       `json:"stop_proven"`
	Checks          []Check    `json:"checks,omitempty"`
	Passed          bool       `json:"passed"`
	FailureStage    string     `json:"failure_stage,omitempty"`
}

// Qualify tests one exact stopped candidate through a fresh disposable clone.
// Failed clones and evidence remain for inspection; this method never deletes
// a backend object or treats a stale stored readiness bit as READY proof.
func (q *Qualifier) Qualify(ctx context.Context, candidate basebuild.Result) (receipt basebuild.QualificationReceipt, err error) {
	if q == nil {
		return receipt, errors.New("qualifier is required")
	}
	if err := validateCandidate(candidate); err != nil {
		return receipt, err
	}
	root, err := openAttempt(candidate.AttemptDirectory, q.deps.ACL)
	if err != nil {
		return receipt, err
	}
	defer root.Close()
	for _, name := range []string{"qualification-evidence.json", "qualification-bom.json"} {
		if _, statErr := root.Lstat(name); statErr == nil {
			return receipt, fmt.Errorf("existing qualification file %q blocks a new attempt", name)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return receipt, statErr
		}
	}
	ev := evidence{Version: 1, CandidateID: candidate.CandidateID, PreparationKey: candidate.PreparationKey}
	stage := "candidate-admission"
	cloneCreated := false
	stopAttempted := false
	name := ""
	var created session.Record
	defer func() {
		if err == nil {
			return
		}
		if cloneCreated && !stopAttempted {
			cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			stopped, stopErr := q.deps.Lifecycle.Stop(cleanup, name)
			if stopErr == nil && (!sameIdentity(stopped, created) || stopped.IntendedState != session.StateStopped || stopped.StartGeneration != "" || stopped.Readiness.Status != session.ReadinessNotReady) {
				stopErr = errors.New("cleanup stop returned mismatched clone")
			}
			if stopErr == nil {
				stopErr = exactStopped(cleanup, q.deps.Observer, created.Backend.ObjectID)
			}
			if stopErr == nil {
				now := q.deps.Now().UTC()
				ev.StoppedAt, ev.StopProven = &now, true
			}
			cancel()
			if stopErr != nil {
				err = errors.Join(err, fmt.Errorf("stop failed qualification clone: %w", stopErr))
			}
		}
		ev.Passed = false
		ev.FailureStage = stage
		if raw, marshalErr := json.Marshal(ev); marshalErr == nil {
			if writeErr := writePrivate(root, candidate.AttemptDirectory, "qualification-evidence.json", append(raw, '\n'), maxEvidenceBytes, q.deps.ACL); writeErr != nil {
				err = errors.Join(err, fmt.Errorf("retain failure evidence: %w", writeErr))
			}
		} else {
			err = errors.Join(err, marshalErr)
		}
	}()
	if err = exactStopped(ctx, q.deps.Observer, candidate.CandidateID); err != nil {
		return receipt, fmt.Errorf("candidate before registration: %w", err)
	}
	stage = "revision-registration"
	registered, err := q.deps.Registrar.RegisterRevision(ctx, candidate.CandidateID)
	if err != nil {
		return receipt, err
	}
	if registered.Version != 1 || registered.Domain != q.deps.Domain || registered.Revision != candidate.CandidateID || registered.Backend.Kind != "tart" || registered.Backend.ObjectID != candidate.CandidateID {
		return receipt, errors.New("registered revision does not match exact candidate")
	}
	stage = "fresh-clone-create"
	name, err = q.deps.NewName()
	if err != nil {
		return receipt, err
	}
	parsed, err := session.ParseName(name)
	if err != nil || string(parsed) != name {
		return receipt, errors.New("qualification session name is invalid")
	}
	creation, err := q.deps.Lifecycle.CreateFreshFromRevision(ctx, name, session.ModeClean, candidate.CandidateID)
	if err != nil {
		return receipt, err
	}
	created = creation.Record
	if !creation.Created || !sameClone(created, q.deps.Domain, name, candidate.CandidateID) || created.Backend.ObjectID == candidate.CandidateID {
		return receipt, errors.New("clone creation did not return a fresh exact stopped session")
	}
	cloneCreated = true
	ev.CloneID, ev.SessionID = created.Backend.ObjectID, created.ID
	if err = exactStopped(ctx, q.deps.Observer, created.Backend.ObjectID); err != nil {
		return receipt, fmt.Errorf("created clone: %w", err)
	}
	stage = "public-start"
	startedAt := q.deps.Now()
	started, err := q.deps.Lifecycle.Start(ctx, name)
	if err != nil {
		return receipt, err
	}
	if !sameIdentity(started, created) || started.IntendedState != session.StateRunning || started.Readiness.Status != session.ReadinessReady || !validUUID(started.StartGeneration) {
		return receipt, errors.New("public start did not return exact ready clone generation")
	}
	ev.Generation = started.StartGeneration
	binding := supervisor.Binding{Domain: string(started.Domain), SessionID: started.ID, BackendKind: started.Backend.Kind, BackendObject: started.Backend.ObjectID, Generation: started.StartGeneration}
	stage = "fresh-ready-snapshot"
	snapshot, err := q.deps.Snapshots.Snapshot(ctx, binding)
	if err != nil {
		return receipt, err
	}
	if !freshReady(snapshot, binding, startedAt, q.deps.Now()) {
		return receipt, errors.New("fresh exact READY evidence is unavailable")
	}
	readyObserved := snapshot.ObservedAt.UTC()
	ev.ReadyObserved = &readyObserved
	stage = "guest-acceptance"
	inspection, err := q.deps.Inspector.Inspect(ctx, InspectionRequest{Session: started, Snapshot: snapshot})
	if err != nil {
		return receipt, err
	}
	if err = validateInspection(inspection, binding, snapshot.ObservedAt, q.deps.Now()); err != nil {
		return receipt, err
	}
	ev.Checks = inspection.Checks
	inspectObserved := inspection.ObservedAt.UTC()
	ev.InspectObserved = &inspectObserved
	stage = "public-stop"
	stopped, err := q.deps.Lifecycle.Stop(ctx, name)
	if err != nil {
		return receipt, err
	}
	if !sameIdentity(stopped, created) || stopped.IntendedState != session.StateStopped || stopped.StartGeneration != "" || stopped.Readiness.Status != session.ReadinessNotReady {
		return receipt, errors.New("public stop did not return exact stopped clone")
	}
	stage = "stopped-observation"
	if err = exactStopped(ctx, q.deps.Observer, created.Backend.ObjectID); err != nil {
		return receipt, err
	}
	stopAttempted = true
	stoppedAt := q.deps.Now().UTC()
	ev.StoppedAt, ev.StopProven = &stoppedAt, true
	ev.Passed = true
	stage = "evidence-persistence"
	bom := append([]byte(nil), inspection.BOM...)
	bom = append(bom, '\n')
	if err = writePrivate(root, candidate.AttemptDirectory, "qualification-bom.json", bom, maxBOMBytes, q.deps.ACL); err != nil {
		return receipt, err
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return receipt, err
	}
	raw = append(raw, '\n')
	if err = writePrivate(root, candidate.AttemptDirectory, "qualification-evidence.json", raw, maxEvidenceBytes, q.deps.ACL); err != nil {
		return receipt, err
	}
	return basebuild.QualificationReceipt{CandidateID: candidate.CandidateID, PreparationKey: candidate.PreparationKey, CloneID: created.Backend.ObjectID, EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), BOMSHA256: fmt.Sprintf("%x", sha256.Sum256(bom)), Passed: true}, nil
}

func sameClone(record session.Record, domainID domain.ID, name, revision string) bool {
	return record.Version == 2 && record.Domain == domainID && string(record.Name) == name && validUUID(record.ID) && record.Mode == session.ModeClean && record.IntendedState == session.StateStopped && record.Backend.Kind == "tart" && record.Backend.ObjectID == "boxwarden-"+string(domainID)+"-"+strings.ReplaceAll(record.ID, "-", "") && record.GoldenRevision == revision && record.StartGeneration == "" && record.Readiness.Status == session.ReadinessNotReady
}

func sameIdentity(a, b session.Record) bool {
	return a.Version == b.Version && a.Domain == b.Domain && a.Name == b.Name && a.ID == b.ID && a.Mode == b.Mode && a.Backend == b.Backend && a.GoldenRevision == b.GoldenRevision
}

func freshReady(snapshot supervisor.Snapshot, binding supervisor.Binding, startedAt, now time.Time) bool {
	return snapshot.Binding == binding && snapshot.BackendRunning && snapshot.SerialHealthy && snapshot.PinPresent && snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches && !snapshot.ObservedAt.IsZero() && !snapshot.ObservedAt.Before(startedAt) && !snapshot.ObservedAt.After(now) && now.Sub(snapshot.ObservedAt) <= maxReadyAge
}

func validateInspection(result Inspection, binding supervisor.Binding, earliest, now time.Time) error {
	if result.Binding != binding || result.ObservedAt.IsZero() || result.ObservedAt.Before(earliest) || result.ObservedAt.After(now) || now.Sub(result.ObservedAt) > maxReadyAge {
		return errors.New("guest inspection is not bound to fresh exact generation")
	}
	if len(result.Checks) == 0 || len(result.Checks) > 128 || len(result.BOM) == 0 || len(result.BOM) > maxBOMBytes-1 || !json.Valid(result.BOM) {
		return errors.New("guest inspection is empty, invalid, or oversized")
	}
	seen := map[string]bool{}
	for _, check := range result.Checks {
		if len(check.Name) == 0 || len(check.Name) > 64 || seen[check.Name] || !check.Passed {
			return errors.New("guest inspection has invalid, duplicate, or failed check")
		}
		for _, c := range check.Name {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return errors.New("guest inspection check name is invalid")
			}
		}
		seen[check.Name] = true
	}
	return nil
}

func exactStopped(ctx context.Context, observer backend.Observer, id string) error {
	observed, err := observer.Observe(ctx, id)
	if err != nil {
		return err
	}
	if observed.ObjectID != id || !observed.Exists || observed.State != backend.ObjectStopped {
		return fmt.Errorf("backend object %q is not observed stopped", id)
	}
	return nil
}

func validateCandidate(candidate basebuild.Result) error {
	if candidate.State != basebuild.CandidateStopped || candidate.Qualified || candidate.CacheAdmitted || backend.ValidateObjectID(candidate.CandidateID) != nil || !lowerDigest(candidate.PreparationKey) || !filepath.IsAbs(candidate.AttemptDirectory) || filepath.Clean(candidate.AttemptDirectory) != candidate.AttemptDirectory || candidate.AttemptDirectory == "/" {
		return errors.New("candidate is not one stopped unqualified exact build result")
	}
	return nil
}

func lowerDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i, c := range raw {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func randomName() (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return "qual" + hex.EncodeToString(nonce[:]), nil
}

func openAttempt(path string, inspector privateacl.Inspector) (*os.Root, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("resolve attempt path: %w", err)
	}
	if resolved != path {
		return nil, errors.New("attempt path has a symlinked component")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(info); err != nil {
		return nil, err
	}
	if err := privateacl.Check(path, info, inspector); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	if !os.SameFile(info, opened) {
		root.Close()
		return nil, errors.New("attempt directory changed during admission")
	}
	if err := privateDirectory(opened); err != nil {
		root.Close()
		return nil, err
	}
	if err := privateacl.Check(path, opened, inspector); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func privateDirectory(info os.FileInfo) error {
	if info == nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("attempt directory is not private")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return errors.New("attempt directory owner changed")
	}
	return nil
}

func writePrivate(root *os.Root, directory, name string, raw []byte, maximum int, inspector privateacl.Inspector) error {
	if len(raw) == 0 || len(raw) > maximum {
		return fmt.Errorf("%s exceeds evidence bound", name)
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 || int(stat.Uid) != os.Getuid() {
		return errors.New("new evidence file is not private")
	}
	if err := privateacl.Check(filepath.Join(directory, name), info, inspector); err != nil {
		return err
	}
	written, err := file.Write(raw)
	if err != nil {
		return err
	}
	if written != len(raw) {
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return syncRoot(root)
}

func syncRoot(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

var _ basebuild.Qualifier = (*Qualifier)(nil)
