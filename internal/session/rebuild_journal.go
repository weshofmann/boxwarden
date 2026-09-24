package session

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

const maxRebuildJournalBytes = 4096

type RebuildPhase string

const (
	RebuildReserved RebuildPhase = "reserved"
	RebuildCloned   RebuildPhase = "cloned"
	RebuildCutover  RebuildPhase = "cutover"
	RebuildReady    RebuildPhase = "ready"
	RebuildRetiring RebuildPhase = "retiring"
)

// RebuildJournal binds one stopped sandbox to one candidate system. It is
// separate from the session record so ordinary lifecycle record writes cannot
// silently erase an unfinished rebuild. Its fields contain no private key.
type RebuildJournal struct {
	Version           int          `json:"version"`
	Domain            domain.ID    `json:"domain"`
	SessionName       string       `json:"session_name"`
	SessionID         string       `json:"session_id"`
	OperationID       string       `json:"operation_id"`
	Phase             RebuildPhase `json:"phase"`
	OldBackend        string       `json:"old_backend"`
	OldRevision       string       `json:"old_revision"`
	CandidateBackend  string       `json:"candidate_backend"`
	CandidateRevision string       `json:"candidate_revision"`
	OldPinPresent     bool         `json:"old_pin_present"`
	// OldPinDigest is SHA-256 of json.Marshal of the fully validated old
	// sshx.HostKeyPin loaded under the exact derived old binding.
	OldPinDigest string `json:"old_pin_digest"`
}

func (j RebuildJournal) oldPinBinding() sshx.Binding {
	return sshx.Binding{Domain: j.Domain, SessionID: j.SessionID, BackendKind: "tart", BackendObject: j.OldBackend}
}

func (j RebuildJournal) verifyOldPinWitness(pin sshx.HostKeyPin) error {
	binding := j.oldPinBinding()
	if !j.OldPinPresent || pin.Version != 1 || pin.Domain != binding.Domain || pin.SessionID != binding.SessionID ||
		pin.BackendKind != binding.BackendKind || pin.BackendObject != binding.BackendObject {
		return fmt.Errorf("old host-key pin does not match exact rebuild binding")
	}
	raw, err := json.Marshal(pin)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != j.OldPinDigest {
		return fmt.Errorf("old host-key pin changed since rebuild reservation")
	}
	return nil
}

func validateRebuildJournal(j RebuildJournal) error {
	parsedDomain, err := domain.Parse(string(j.Domain))
	if err != nil || parsedDomain != j.Domain {
		return fmt.Errorf("invalid rebuild domain: %v", err)
	}
	name, err := ParseName(j.SessionName)
	if err != nil || string(name) != j.SessionName || j.Version != 1 || !validUUID(j.SessionID) || !validUUID(j.OperationID) ||
		!validBackendObjectID(j.OldBackend) || !validBackendObjectID(j.OldRevision) ||
		!validBackendObjectID(j.CandidateBackend) || !validBackendObjectID(j.CandidateRevision) ||
		j.CandidateBackend != objectIDFor(j.Domain, j.OperationID) || j.CandidateBackend == j.OldBackend {
		return fmt.Errorf("invalid rebuild identity or backend binding")
	}
	switch j.Phase {
	case RebuildReserved, RebuildCloned, RebuildCutover, RebuildReady, RebuildRetiring:
	default:
		return fmt.Errorf("invalid rebuild phase %q", j.Phase)
	}
	if !j.OldPinPresent && j.OldPinDigest != "" || j.OldPinPresent && !lowerSHA256(j.OldPinDigest) {
		return fmt.Errorf("invalid old host-key pin witness")
	}
	return nil
}

func lowerSHA256(raw string) bool {
	if len(raw) != 64 {
		return false
	}
	for _, c := range raw {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// LoadRebuildJournal returns os.ErrNotExist only when no journal exists.
// Malformed, foreign, or unsafe journal state blocks ordinary operations.
func LoadRebuildJournal(stateRoot string, expectedDomain domain.ID, sessionName string) (RebuildJournal, error) {
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return RebuildJournal{}, err
	}
	name, err := ParseName(sessionName)
	if err != nil {
		return RebuildJournal{}, err
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return RebuildJournal{}, err
	}
	defer root.Close()
	rebuilds, err := openSessionChild(root, "rebuilds", false)
	if err != nil {
		return RebuildJournal{}, err
	}
	defer rebuilds.Close()
	file, err := openSessionPrivateRegular(rebuilds, string(name)+".json")
	if err != nil {
		return RebuildJournal{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxRebuildJournalBytes+1))
	if err != nil || len(raw) > maxRebuildJournalBytes {
		return RebuildJournal{}, fmt.Errorf("read bounded rebuild journal: %v", err)
	}
	j, err := decodeRebuildJournal(raw)
	if err != nil {
		return RebuildJournal{}, err
	}
	if j.Domain != expectedDomain || j.SessionName != string(name) {
		return RebuildJournal{}, fmt.Errorf("rebuild journal does not match requested domain and session")
	}
	return j, nil
}

// RequireNoRebuild is a read-only gate. Mutating callers invoke it while
// holding their ordinary session lock, after any journal writer takes that
// same lock. An existing or corrupt journal fails closed.
func RequireNoRebuild(stateRoot string, domainID domain.ID, sessionName string) error {
	_, err := LoadRebuildJournal(stateRoot, domainID, sessionName)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect session rebuild journal: %w", err)
	}
	return fmt.Errorf("session %q has a pending system rebuild", sessionName)
}

// The caller holds the domain golden lock, which also serializes creation of
// candidate journals and ordinary session backend reservations.
func requireUnreservedRebuildObject(stateRoot string, domainID domain.ID, objectID string) error {
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	rebuilds, err := openSessionChild(root, "rebuilds", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer rebuilds.Close()
	directory, err := rebuilds.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return fmt.Errorf("unexpected rebuild registry entry %q", entry.Name())
		}
		name, err := ParseName(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return fmt.Errorf("invalid rebuild registry entry %q: %w", entry.Name(), err)
		}
		journal, err := LoadRebuildJournal(stateRoot, domainID, string(name))
		if err != nil {
			return fmt.Errorf("inspect rebuild registry entry %q: %w", entry.Name(), err)
		}
		if journal.OldBackend == objectID || journal.CandidateBackend == objectID {
			return fmt.Errorf("backend object identity is reserved by rebuild for session %q", name)
		}
	}
	return nil
}

// The caller holds the exact session transition and session locks and has
// checked stopped intent, attachment Uses, pin witness, and candidate identity
// reservation. Exclusive creation persists intent before any backend mutation.
func createRebuildJournal(stateRoot string, j RebuildJournal) error {
	if err := validateRebuildJournal(j); err != nil {
		return err
	}
	if j.Phase != RebuildReserved {
		return fmt.Errorf("new rebuild journal must reserve the candidate first")
	}
	if err := requireUnreservedBackendObject(stateRoot, j.Domain, Name(j.SessionName), j.CandidateBackend); err != nil {
		return fmt.Errorf("reserve rebuild candidate backend: %w", err)
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	rebuilds, err := openSessionChild(root, "rebuilds", true)
	if err != nil {
		return err
	}
	defer rebuilds.Close()
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if len(raw) > maxRebuildJournalBytes {
		return fmt.Errorf("rebuild journal exceeds bound")
	}
	file, err := rebuilds.OpenFile(j.SessionName+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err // preserve uncertain state for explicit reconciliation
	}
	return sessionSyncRoot(rebuilds)
}

// advanceRebuildJournal changes only the phase under the exact session and
// transition locks. The old version is checked before an atomic replacement;
// a failed post-rename sync leaves the new phase visible for retry.
func advanceRebuildJournal(stateRoot string, expected, next RebuildJournal) error {
	if err := validateRebuildJournal(expected); err != nil {
		return err
	}
	if err := validateRebuildJournal(next); err != nil {
		return err
	}
	want := expected
	switch expected.Phase {
	case RebuildReserved:
		want.Phase = RebuildCloned
	case RebuildCloned:
		want.Phase = RebuildCutover
	case RebuildCutover:
		want.Phase = RebuildReady
	case RebuildReady:
		want.Phase = RebuildRetiring
	default:
		return fmt.Errorf("rebuild phase %q cannot advance", expected.Phase)
	}
	if next != want {
		return fmt.Errorf("rebuild journal transition changed identity or skipped phase")
	}
	current, err := LoadRebuildJournal(stateRoot, expected.Domain, expected.SessionName)
	if err != nil {
		return err
	}
	if current == next {
		return nil // prior atomic rename became visible before a failed sync
	}
	if current != expected {
		return fmt.Errorf("rebuild journal changed before phase advancement")
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	rebuilds, err := openSessionChild(root, "rebuilds", false)
	if err != nil {
		return err
	}
	defer rebuilds.Close()
	target := expected.SessionName + ".json"
	temporaryName, err := sessionTemporaryName(target)
	if err != nil {
		return err
	}
	temporary, err := rebuilds.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer rebuilds.Remove(temporaryName)
	raw, err := json.Marshal(next)
	if err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := rebuilds.Rename(temporaryName, target); err != nil {
		return err
	}
	return sessionSyncRoot(rebuilds)
}

// removeRebuildJournal is the final retirement step. The caller holds the
// transition and session locks and has re-observed the exact old object absent.
func removeRebuildJournal(stateRoot string, expected RebuildJournal) error {
	if err := validateRebuildJournal(expected); err != nil || expected.Phase != RebuildRetiring {
		return fmt.Errorf("invalid completed rebuild journal: %v", err)
	}
	current, err := LoadRebuildJournal(stateRoot, expected.Domain, expected.SessionName)
	if err != nil || current != expected {
		return fmt.Errorf("rebuild journal changed before final clearance: %v", err)
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	rebuilds, err := openSessionChild(root, "rebuilds", false)
	if err != nil {
		return err
	}
	defer rebuilds.Close()
	if err := rebuilds.Remove(expected.SessionName + ".json"); err != nil {
		return err
	}
	return sessionSyncRoot(rebuilds)
}

func decodeRebuildJournal(raw []byte) (RebuildJournal, error) {
	var j RebuildJournal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := requireObjectStart(decoder); err != nil {
		return j, err
	}
	seen := map[string]bool{}
	for decoder.More() {
		field, err := objectField(decoder, seen)
		if err != nil {
			return RebuildJournal{}, err
		}
		switch field {
		case "version":
			err = decoder.Decode(&j.Version)
		case "domain":
			err = decoder.Decode(&j.Domain)
		case "session_name":
			err = decoder.Decode(&j.SessionName)
		case "session_id":
			err = decoder.Decode(&j.SessionID)
		case "operation_id":
			err = decoder.Decode(&j.OperationID)
		case "phase":
			err = decoder.Decode(&j.Phase)
		case "old_backend":
			err = decoder.Decode(&j.OldBackend)
		case "old_revision":
			err = decoder.Decode(&j.OldRevision)
		case "candidate_backend":
			err = decoder.Decode(&j.CandidateBackend)
		case "candidate_revision":
			err = decoder.Decode(&j.CandidateRevision)
		case "old_pin_present":
			err = decoder.Decode(&j.OldPinPresent)
		case "old_pin_digest":
			err = decoder.Decode(&j.OldPinDigest)
		default:
			return RebuildJournal{}, fmt.Errorf("unknown rebuild journal field %q", field)
		}
		if err != nil {
			return RebuildJournal{}, fmt.Errorf("rebuild journal field %q: %w", field, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return RebuildJournal{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		return RebuildJournal{}, fmt.Errorf("trailing rebuild journal data %v: %v", token, err)
	}
	for _, required := range []string{"version", "domain", "session_name", "session_id", "operation_id", "phase", "old_backend", "old_revision", "candidate_backend", "candidate_revision", "old_pin_present", "old_pin_digest"} {
		if !seen[required] {
			return RebuildJournal{}, fmt.Errorf("missing rebuild journal field %q", required)
		}
	}
	if err := validateRebuildJournal(j); err != nil {
		return RebuildJournal{}, err
	}
	return j, nil
}
