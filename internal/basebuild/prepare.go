package basebuild

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const preparedRecordVersion = 2

// The production inspector is always the platform ACL authority. Package
// tests replace this unexported seam to exercise journal logic on Linux,
// where host admission correctly remains unsupported.
var basebuildACLInspector privateacl.Inspector = privateacl.OSInspector{}

type PrepareRequest struct {
	StateRoot string
	Inputs    Inputs
}

// Qualifier must actually test a fresh clone of the stopped candidate. It
// writes bounded, private, one-link regular files named qualification-evidence.json
// and qualification-bom.json directly in Result.AttemptDirectory; the returned
// hashes must match those bytes. This interface has no production adapter yet.
type Qualifier interface {
	Qualify(context.Context, Result) (QualificationReceipt, error)
}

type QualificationReceipt struct {
	CandidateID    string `json:"candidate_id"`
	PreparationKey string `json:"preparation_key"`
	CloneID        string `json:"clone_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	BOMSHA256      string `json:"bom_sha256"`
	Passed         bool   `json:"passed"`
}

// PreparedRecord is the one authoritative cache entry for a preparation key.
// It is local trusted-host metadata, not a cryptographic attestation of a VM.
type PreparedRecord struct {
	Version           int                  `json:"version"`
	PreparationKey    string               `json:"preparation_key"`
	CandidateID       string               `json:"candidate_id"`
	CandidateIdentity string               `json:"candidate_identity"`
	AttemptDirectory  string               `json:"attempt_directory"`
	Qualification     QualificationReceipt `json:"qualification"`
}

// CandidateIdentity binds admission to one stopped backend object instance.
// A name or process ID is insufficient. Implementations must fail on unknown
// layout and return a lowercase SHA-256 digest of stable object facts/bytes.
type CandidateIdentity interface {
	CandidateIdentity(context.Context, string) (string, error)
}

type PreparedDisposition string

const (
	PreparedBuilt  PreparedDisposition = "built"
	PreparedReused PreparedDisposition = "reused"
)

type PreparedResult struct {
	Disposition PreparedDisposition
	Record      PreparedRecord
}

type PrepareDependencies struct {
	Build     Dependencies
	Qualifier Qualifier
}

// Prepare verifies exact source intent on every call. Under one state-root
// lock, it selects a stopped qualified entry or builds a fresh candidate and
// admits it only after fresh-clone qualification. Failed attempts are retained
// and never adopted by key alone.
func Prepare(ctx context.Context, request PrepareRequest, deps PrepareDependencies) (PreparedResult, error) {
	if deps.Qualifier == nil {
		return PreparedResult{}, errors.New("fresh-clone qualifier is required")
	}
	if deps.Build.Checks == nil || deps.Build.VM == nil {
		return PreparedResult{}, errors.New("input checks and VM observer are required")
	}
	identity, ok := deps.Build.VM.(CandidateIdentity)
	if !ok {
		return PreparedResult{}, errors.New("candidate object identity verifier is required")
	}
	if !absoluteClean(request.StateRoot) {
		return PreparedResult{}, errors.New("state root must be canonical and absolute")
	}
	if err := backend.ValidateObjectID(request.Inputs.AttemptID); err != nil {
		return PreparedResult{}, err
	}
	if err := backend.ValidateObjectID(request.Inputs.CandidateID); err != nil {
		return PreparedResult{}, err
	}
	if !absoluteClean(request.Inputs.AttemptRoot) {
		return PreparedResult{}, errors.New("attempt root must be canonical and absolute")
	}
	if err := privateStateRoot(request.Inputs.AttemptRoot); err != nil {
		return PreparedResult{}, err
	}
	if err := privateStateRoot(request.StateRoot); err != nil {
		return PreparedResult{}, err
	}
	key, err := deps.Build.Checks.Verify(request.Inputs)
	if err != nil {
		return PreparedResult{}, fmt.Errorf("verify preparation inputs: %w", err)
	}
	if !lowerHexDigest(key) {
		return PreparedResult{}, errors.New("verification returned an invalid preparation key")
	}

	held, err := lock.Acquire(ctx, request.StateRoot, "prepared-base")
	if err != nil {
		return PreparedResult{}, err
	}
	defer held.Release()
	record, found, err := loadPrepared(request.StateRoot, key)
	if err != nil {
		return PreparedResult{}, err
	}
	if found {
		if err := verifyAdmittedAttempt(record); err != nil {
			return PreparedResult{}, fmt.Errorf("cached attempt: %w", err)
		}
		if err := verifyQualificationFiles(record.AttemptDirectory, record.Qualification); err != nil {
			return PreparedResult{}, fmt.Errorf("cached qualification: %w", err)
		}
		if err := exactStopped(ctx, deps.Build.VM, record.CandidateID); err != nil {
			return PreparedResult{}, fmt.Errorf("cached candidate: %w", err)
		}
		if err := exactCandidateIdentity(ctx, identity, record.CandidateID, record.CandidateIdentity); err != nil {
			return PreparedResult{}, fmt.Errorf("cached candidate identity: %w", err)
		}
		if err := completeAdmission(record); err != nil {
			return PreparedResult{}, err
		}
		return PreparedResult{Disposition: PreparedReused, Record: record}, nil
	}
	if recovered, ok, err := resumePendingAdmission(ctx, request, key, deps.Build.VM, identity); err != nil {
		return PreparedResult{}, err
	} else if ok {
		return PreparedResult{Disposition: PreparedReused, Record: recovered}, nil
	}
	if deps.Build.Seed == nil {
		return PreparedResult{}, errors.New("seed builder is required for cache miss")
	}
	build, err := Build(ctx, request.Inputs, deps.Build)
	if err != nil {
		return PreparedResult{}, err
	}
	if build.PreparationKey != key || build.CandidateID != request.Inputs.CandidateID || build.State != CandidateStopped || build.Qualified || build.CacheAdmitted {
		return PreparedResult{}, errors.New("candidate build result violates unqualified state contract")
	}
	state, err := ReadAttempt(build.AttemptDirectory)
	if err != nil {
		return PreparedResult{}, err
	}
	if state.Phase != PhaseCandidateStopped || state.PreparationKey != key || state.CandidateID != build.CandidateID {
		return PreparedResult{}, errors.New("candidate attempt journal does not match build result")
	}
	state.Phase = PhaseQualifying
	if err := writeAttempt(build.AttemptDirectory, state); err != nil {
		return PreparedResult{}, err
	}
	qualificationFailed := func(cause error) (PreparedResult, error) {
		state.Phase = PhaseFailed
		state.Failure = "qualification failed"
		return PreparedResult{}, errors.Join(cause, writeAttempt(build.AttemptDirectory, state))
	}
	receipt, err := deps.Qualifier.Qualify(ctx, build)
	if err != nil {
		return qualificationFailed(fmt.Errorf("qualify fresh clone: %w", err))
	}
	if err := validReceipt(receipt, key, build.CandidateID); err != nil {
		return qualificationFailed(err)
	}
	if err := verifyQualificationFiles(build.AttemptDirectory, receipt); err != nil {
		return qualificationFailed(err)
	}
	if err := exactStopped(ctx, deps.Build.VM, build.CandidateID); err != nil {
		return qualificationFailed(fmt.Errorf("candidate after qualification: %w", err))
	}
	objectIdentity, err := identity.CandidateIdentity(ctx, build.CandidateID)
	if err != nil {
		return qualificationFailed(fmt.Errorf("candidate identity unavailable: %w", err))
	}
	if !lowerHexDigest(objectIdentity) {
		return qualificationFailed(errors.New("candidate identity is invalid"))
	}
	record = PreparedRecord{Version: preparedRecordVersion, PreparationKey: key, CandidateID: build.CandidateID, CandidateIdentity: objectIdentity, AttemptDirectory: build.AttemptDirectory, Qualification: receipt}
	state.Phase = PhaseAdmissionPending
	state.Qualification = &receipt
	state.CandidateIdentity = objectIdentity
	if err := writeAttempt(build.AttemptDirectory, state); err != nil {
		return PreparedResult{}, fmt.Errorf("persist qualification admission intent: %w", err)
	}
	if err := storePrepared(request.StateRoot, record); err != nil {
		return PreparedResult{}, fmt.Errorf("admit prepared candidate: %w", err)
	}
	if err := completeAdmission(record); err != nil {
		return PreparedResult{}, fmt.Errorf("record admitted but attempt journal update failed: %w", err)
	}
	return PreparedResult{Disposition: PreparedBuilt, Record: record}, nil
}

func validReceipt(receipt QualificationReceipt, key, candidateID string) error {
	if !receipt.Passed || receipt.CandidateID != candidateID || receipt.PreparationKey != key || backend.ValidateObjectID(receipt.CloneID) != nil || receipt.CloneID == candidateID || !lowerHexDigest(receipt.EvidenceSHA256) || !lowerHexDigest(receipt.BOMSHA256) {
		return errors.New("qualification receipt is incomplete or mismatched")
	}
	return nil
}

func lowerHexDigest(value string) bool {
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

func verifyAdmittedAttempt(record PreparedRecord) error {
	if !absoluteClean(record.AttemptDirectory) {
		return errors.New("attempt directory is invalid")
	}
	if err := privateStateRoot(record.AttemptDirectory); err != nil {
		return err
	}
	state, err := ReadAttempt(record.AttemptDirectory)
	if err != nil {
		return err
	}
	if (state.Phase != PhaseAdmitted && state.Phase != PhaseAdmissionPending) || state.CandidateID != record.CandidateID || state.CandidateIdentity != record.CandidateIdentity || state.PreparationKey != record.PreparationKey || state.Qualification == nil || *state.Qualification != record.Qualification {
		return errors.New("admitted attempt journal does not match cache record")
	}
	return nil
}

// A pending journal is a durable receipt of completed qualification, not a
// failed qualification checkpoint. Recheck all independent evidence before
// publishing or completing the cache record.
func resumePendingAdmission(ctx context.Context, request PrepareRequest, key string, observer backend.Observer, identity CandidateIdentity) (PreparedRecord, bool, error) {
	dir := filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID)
	if !absoluteClean(dir) {
		return PreparedRecord{}, false, errors.New("attempt directory is invalid")
	}
	state, err := ReadAttempt(dir)
	if errors.Is(err, os.ErrNotExist) {
		return PreparedRecord{}, false, nil
	}
	if err != nil {
		return PreparedRecord{}, false, err
	}
	if state.Phase != PhaseAdmissionPending || state.PreparationKey != key || state.CandidateID != request.Inputs.CandidateID || state.Qualification == nil {
		return PreparedRecord{}, false, errors.New("existing attempt is not a matching pending admission")
	}
	record := PreparedRecord{Version: preparedRecordVersion, PreparationKey: key, CandidateID: state.CandidateID, CandidateIdentity: state.CandidateIdentity, AttemptDirectory: dir, Qualification: *state.Qualification}
	if !lowerHexDigest(record.CandidateIdentity) {
		return PreparedRecord{}, false, errors.New("pending candidate identity is invalid")
	}
	if err := validReceipt(record.Qualification, key, record.CandidateID); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := verifyQualificationFiles(dir, record.Qualification); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := exactStopped(ctx, observer, record.CandidateID); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := exactCandidateIdentity(ctx, identity, record.CandidateID, record.CandidateIdentity); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := storePrepared(request.StateRoot, record); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := completeAdmission(record); err != nil {
		return PreparedRecord{}, false, err
	}
	return record, true, nil
}

func completeAdmission(record PreparedRecord) error {
	state, err := ReadAttempt(record.AttemptDirectory)
	if err != nil {
		return err
	}
	if state.CandidateID != record.CandidateID || state.CandidateIdentity != record.CandidateIdentity || state.PreparationKey != record.PreparationKey || state.Qualification == nil || *state.Qualification != record.Qualification || (state.Phase != PhaseAdmissionPending && state.Phase != PhaseAdmitted) {
		return errors.New("admission journal changed")
	}
	if state.Phase == PhaseAdmitted {
		return nil
	}
	if err := clearUnpublishedAttemptJournal(record.AttemptDirectory); err != nil {
		return err
	}
	state.Phase = PhaseAdmitted
	return writeAttempt(record.AttemptDirectory, state)
}

func clearUnpublishedAttemptJournal(dir string) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	const name = "attempt.json.next"
	entry, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := privateRegular(filepath.Join(dir, name), entry); err != nil {
		return err
	}
	if err := root.Remove(name); err != nil {
		return err
	}
	return syncPreparedRoot(root)
}

func verifyQualificationFiles(attemptDir string, receipt QualificationReceipt) error {
	if !absoluteClean(attemptDir) {
		return errors.New("qualification attempt directory is invalid")
	}
	if err := privateStateRoot(attemptDir); err != nil {
		return err
	}
	if err := verifyQualificationFile(attemptDir, "qualification-evidence.json", receipt.EvidenceSHA256, 8<<20); err != nil {
		return fmt.Errorf("qualification evidence: %w", err)
	}
	if err := verifyQualificationFile(attemptDir, "qualification-bom.json", receipt.BOMSHA256, 1<<20); err != nil {
		return fmt.Errorf("qualification BOM: %w", err)
	}
	return nil
}

func verifyQualificationFile(attemptDir, name, expected string, maximum int64) error {
	root, err := os.OpenRoot(attemptDir)
	if err != nil {
		return err
	}
	defer root.Close()
	entry, err := root.Lstat(name)
	if err != nil {
		return err
	}
	if err := privateRegular(filepath.Join(attemptDir, name), entry); err != nil {
		return err
	}
	if entry.Size() == 0 || entry.Size() > maximum {
		return errors.New("evidence file is empty or exceeds limit")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(entry, opened) {
		return errors.New("evidence file changed while opening")
	}
	hash := sha256.New()
	copied, err := io.Copy(hash, io.LimitReader(file, maximum+1))
	if err != nil {
		return err
	}
	if copied != entry.Size() || copied > maximum {
		return errors.New("evidence file changed or exceeded limit while reading")
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expected {
		return errors.New("evidence digest mismatch")
	}
	return nil
}

func exactStopped(ctx context.Context, observer backend.Observer, candidateID string) error {
	observation, err := observer.Observe(ctx, candidateID)
	if err != nil {
		return err
	}
	if observation.ObjectID != candidateID || !observation.Exists || observation.State != backend.ObjectStopped {
		return errors.New("exact candidate is not observed stopped")
	}
	return nil
}

func exactCandidateIdentity(ctx context.Context, verifier CandidateIdentity, candidateID, expected string) error {
	if !lowerHexDigest(expected) {
		return errors.New("recorded candidate identity is invalid")
	}
	actual, err := verifier.CandidateIdentity(ctx, candidateID)
	if err != nil {
		return err
	}
	if actual != expected {
		return errors.New("candidate object identity changed")
	}
	return nil
}

func privateStateRoot(path string) error {
	entry, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !entry.IsDir() || entry.Mode().Perm() != 0700 {
		return errors.New("state root must be a private directory")
	}
	if err := privateDirectory(path, entry); err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if resolved != path {
		return errors.New("state root has a symlinked ancestor")
	}
	return nil
}

func loadPrepared(stateRoot, key string) (PreparedRecord, bool, error) {
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		return PreparedRecord{}, false, err
	}
	defer root.Close()
	prepared, err := openPreparedChild(root, stateRoot, "prepared", false)
	if errors.Is(err, os.ErrNotExist) {
		return PreparedRecord{}, false, nil
	}
	if err != nil {
		return PreparedRecord{}, false, err
	}
	defer prepared.Close()
	records, err := openPreparedChild(prepared, filepath.Join(stateRoot, "prepared"), "records", false)
	if errors.Is(err, os.ErrNotExist) {
		return PreparedRecord{}, false, nil
	}
	if err != nil {
		return PreparedRecord{}, false, err
	}
	defer records.Close()
	name := key + ".json"
	if err := finishPublishedRecord(records, filepath.Join(stateRoot, "prepared", "records"), name); err != nil {
		return PreparedRecord{}, false, err
	}
	entry, err := records.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return PreparedRecord{}, false, nil
	}
	if err != nil {
		return PreparedRecord{}, false, err
	}
	if err := privateRegular(filepath.Join(stateRoot, "prepared", "records", name), entry); err != nil {
		return PreparedRecord{}, false, err
	}
	file, err := records.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return PreparedRecord{}, false, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return PreparedRecord{}, false, err
	}
	if !os.SameFile(entry, opened) {
		return PreparedRecord{}, false, errors.New("prepared record changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return PreparedRecord{}, false, err
	}
	if len(data) > 4096 {
		return PreparedRecord{}, false, errors.New("prepared record exceeds limit")
	}
	var record PreparedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return PreparedRecord{}, false, err
	}
	canonical, err := json.Marshal(record)
	if err != nil {
		return PreparedRecord{}, false, err
	}
	if !bytes.Equal(data, canonical) {
		return PreparedRecord{}, false, errors.New("prepared record is not canonical")
	}
	if record.Version != preparedRecordVersion || record.PreparationKey != key || record.CandidateID != record.Qualification.CandidateID || !lowerHexDigest(record.CandidateIdentity) || !absoluteClean(record.AttemptDirectory) {
		return PreparedRecord{}, false, errors.New("prepared record identity mismatch")
	}
	if err := backend.ValidateObjectID(record.CandidateID); err != nil {
		return PreparedRecord{}, false, err
	}
	if err := validReceipt(record.Qualification, key, record.CandidateID); err != nil {
		return PreparedRecord{}, false, err
	}
	return record, true, nil
}

func storePrepared(stateRoot string, record PreparedRecord) error {
	return storePreparedWithHook(stateRoot, record, nil)
}

func storePreparedWithHook(stateRoot string, record PreparedRecord, afterLink func() error) error {
	if existing, found, err := loadPrepared(stateRoot, record.PreparationKey); err != nil {
		return err
	} else if found {
		if existing != record {
			return errors.New("preparation key already belongs to a different candidate")
		}
		return nil
	}
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	prepared, err := openPreparedChild(root, stateRoot, "prepared", true)
	if err != nil {
		return err
	}
	defer prepared.Close()
	records, err := openPreparedChild(prepared, filepath.Join(stateRoot, "prepared"), "records", true)
	if err != nil {
		return err
	}
	defer records.Close()
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	name := record.PreparationKey + ".json"
	temporary := name + ".next"
	if old, err := records.Lstat(temporary); err == nil {
		if err := privateRegular(filepath.Join(stateRoot, "prepared", "records", temporary), old); err != nil {
			return err
		}
		// This is an unpublished scratch file. A crash may leave any prefix
		// of the record here; the pending journal is the durable authority.
		if err := records.Remove(temporary); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := records.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	linked := false
	defer func() {
		if !linked {
			_ = records.Remove(temporary)
		}
	}()
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := records.Link(temporary, name); err != nil {
		return fmt.Errorf("publish prepared record without replacement: %w", err)
	}
	linked = true
	if afterLink != nil {
		if err := afterLink(); err != nil {
			return err
		}
	}
	if err := records.Remove(temporary); err != nil {
		return err
	}
	return syncPreparedRoot(records)
}

func finishPublishedRecord(records *os.Root, recordsPath, name string) error {
	temporary := name + ".next"
	published, err := records.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	staged, err := records.Lstat(temporary)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	stat, ok := published.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 2 || !os.SameFile(published, staged) || published.Mode().Perm() != 0600 || !published.Mode().IsRegular() || stat.Uid != uint32(os.Getuid()) {
		return errors.New("interrupted prepared publication has unsafe identity")
	}
	if err := privateacl.Check(filepath.Join(recordsPath, name), published, basebuildACLInspector); err != nil {
		return err
	}
	if err := privateacl.Check(filepath.Join(recordsPath, temporary), staged, basebuildACLInspector); err != nil {
		return err
	}
	if err := records.Remove(temporary); err != nil {
		return err
	}
	return syncPreparedRoot(records)
}

func openPreparedChild(parent *os.Root, parentPath, name string, create bool) (*os.Root, error) {
	entry, err := parent.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err := parent.Mkdir(name, 0700); err != nil {
			return nil, err
		}
		if err := syncPreparedRoot(parent); err != nil {
			return nil, err
		}
		entry, err = parent.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() || entry.Mode().Perm() != 0700 {
		return nil, fmt.Errorf("%s must be a private directory", name)
	}
	if err := privateDirectory(filepath.Join(parentPath, name), entry); err != nil {
		return nil, err
	}
	child, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(entry, opened) {
		child.Close()
		return nil, errors.New("prepared directory changed while opening")
	}
	return child, nil
}

func privateRegular(path string, entry os.FileInfo) error {
	if !entry.Mode().IsRegular() || entry.Mode().Perm() != 0600 {
		return errors.New("prepared record must be a private regular file")
	}
	stat, ok := entry.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		return errors.New("prepared record has unsafe ownership or link count")
	}
	return privateacl.Check(path, entry, basebuildACLInspector)
}

func privateDirectory(path string, entry os.FileInfo) error {
	stat, ok := entry.Sys().(*syscall.Stat_t)
	if !entry.IsDir() || entry.Mode().Perm() != 0700 || !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink < 1 {
		return errors.New("prepared directory has unsafe ownership or mode")
	}
	return privateacl.Check(path, entry, basebuildACLInspector)
}

func syncPreparedRoot(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
