package session

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
)

type storeStage string

const (
	storeBeforeRename storeStage = "before_rename"
	storeAfterRename  storeStage = "after_rename"
)

type storeHook func(storeStage) error

// SaveRecord atomically persists one complete domain-owned session record.
// Callers must hold the session's operation lock for lifecycle transitions.
func SaveRecord(stateRoot string, expectedDomain domain.ID, record Record) error {
	return saveRecord(stateRoot, expectedDomain, record, nil)
}

func saveRecord(stateRoot string, expectedDomain domain.ID, record Record, hook storeHook) error {
	if err := validateRecordForStore(expectedDomain, record); err != nil {
		return err
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	sessions, err := openSessionChild(root, "sessions", true)
	if err != nil {
		return fmt.Errorf("session directory: %w", err)
	}
	defer sessions.Close()
	target := string(record.Name) + ".json"
	if info, err := sessions.Lstat(target); err == nil {
		if err := requirePrivateRegularInfo(info); err != nil {
			return fmt.Errorf("session record %q: %w", record.Name, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect session record %q: %w", record.Name, err)
	}

	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record: %w", err)
	}
	raw = append(raw, '\n')

	temporaryName, err := sessionTemporaryName(target)
	if err != nil {
		return fmt.Errorf("name temporary session record: %w", err)
	}
	temporary, err := sessions.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create temporary session record: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = temporary.Close()
		}
		_ = sessions.Remove(temporaryName)
	}()
	if _, err := temporary.Write(raw); err != nil {
		return fmt.Errorf("write temporary session record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary session record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary session record: %w", err)
	}
	closed = true
	if hook != nil {
		if err := hook(storeBeforeRename); err != nil {
			return fmt.Errorf("before session record rename: %w", err)
		}
	}
	if err := sessions.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("replace session record: %w", err)
	}
	if hook != nil {
		if err := hook(storeAfterRename); err != nil {
			return fmt.Errorf("after session record rename: %w", err)
		}
	}
	if err := sessionSyncRoot(sessions); err != nil {
		return fmt.Errorf("sync session directory: %w", err)
	}
	return nil
}

func validateRecordForStore(expectedDomain domain.ID, record Record) error {
	parsedDomain, err := domain.Parse(string(expectedDomain))
	if err != nil {
		return err
	}
	if record.Domain != parsedDomain {
		return fmt.Errorf("session record domain %q does not match target domain %q", record.Domain, parsedDomain)
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode session record for validation: %w", err)
	}
	decoded, err := decodeRecord(json.NewDecoder(bytes.NewReader(raw)))
	if err != nil {
		return err
	}
	if decoded != record {
		return fmt.Errorf("session record does not round-trip through its versioned schema")
	}
	if record.GoldenRevision != "" && !validBackendObjectID(record.GoldenRevision) {
		return fmt.Errorf("invalid golden revision")
	}
	return nil
}

// requireUnreservedBackendObject checks the durable session registry while the
// caller holds the domain golden lock. That lock serializes first-time UUID
// reservations across otherwise independent per-session locks.
func requireUnreservedBackendObject(stateRoot string, expectedDomain domain.ID, name Name, objectID string) error {
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	sessions, err := openSessionChild(root, "sessions", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("session directory: %w", err)
	}
	defer sessions.Close()
	directory, err := sessions.Open(".")
	if err != nil {
		return fmt.Errorf("open session directory: %w", err)
	}
	entries, err := directory.ReadDir(-1)
	closeErr := directory.Close()
	if err != nil {
		return fmt.Errorf("read session directory: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close session directory: %w", closeErr)
	}
	for _, entry := range entries {
		entryName := entry.Name()
		if strings.HasPrefix(entryName, ".") && strings.Contains(entryName, ".tmp-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(entryName, ".json") {
			return fmt.Errorf("unexpected session registry entry %q", entryName)
		}
		owner, err := ParseName(strings.TrimSuffix(entryName, ".json"))
		if err != nil {
			return fmt.Errorf("invalid session registry entry %q: %w", entryName, err)
		}
		record, err := loadRecordFromRoot(sessions, expectedDomain, owner)
		if err != nil {
			return fmt.Errorf("load session registry entry %q: %w", entryName, err)
		}
		if owner != name && record.Backend.ObjectID == objectID {
			return fmt.Errorf("backend object identity is already reserved by session %q", owner)
		}
	}
	return nil
}

func sessionTemporaryName(target string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "." + target + ".tmp-" + hex.EncodeToString(raw), nil
}
