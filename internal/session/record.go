package session

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const recordVersion = 1

type Mode string

const (
	ModeClean      Mode = "clean"
	ModeQuarantine Mode = "quarantine"
)

type IntendedState string

const (
	StateCreating IntendedState = "creating"
	StateStopped  IntendedState = "stopped"
	StateStarting IntendedState = "starting"
	StateRunning  IntendedState = "running"
	StateStopping IntendedState = "stopping"
	StateDeleting IntendedState = "deleting"
	StateFailed   IntendedState = "failed"
)

type BackendRef struct {
	Kind     string `json:"kind"`
	ObjectID string `json:"object_id"`
}

type Record struct {
	Version        int           `json:"version"`
	Domain         domain.ID     `json:"domain"`
	Name           Name          `json:"name"`
	ID             string        `json:"id"`
	Mode           Mode          `json:"mode"`
	IntendedState  IntendedState `json:"intended_state"`
	Backend        BackendRef    `json:"backend"`
	GoldenRevision string        `json:"golden_revision,omitempty"`
}

func LoadRecord(stateRoot, expectedDomain, rawName string) (Record, error) {
	domainID, err := domain.Parse(expectedDomain)
	if err != nil {
		return Record{}, err
	}
	name, err := ParseName(rawName)
	if err != nil {
		return Record{}, err
	}
	if err := requirePrivateDirectory(stateRoot); err != nil {
		return Record{}, fmt.Errorf("state root: %w", err)
	}
	sessionsRoot := filepath.Join(stateRoot, "sessions")
	if err := requirePrivateDirectory(sessionsRoot); err != nil {
		return Record{}, fmt.Errorf("session directory: %w", err)
	}

	path := filepath.Join(sessionsRoot, string(name)+".json")
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, fmt.Errorf("session record %q: %w", name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Record{}, fmt.Errorf("session record %q must be a regular non-symlink file", name)
	}

	file, err := os.Open(path)
	if err != nil {
		return Record{}, fmt.Errorf("open session record %q: %w", name, err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	record, err := decodeRecord(decoder)
	if err != nil {
		return Record{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Record{}, fmt.Errorf("read session record tail: %w", err)
		}
		return Record{}, fmt.Errorf("unexpected session record token %v", token)
	}
	if record.Domain != domainID {
		return Record{}, fmt.Errorf("session record domain %q does not match requested domain %q", record.Domain, domainID)
	}
	if record.Name != name {
		return Record{}, fmt.Errorf("session record name %q does not match requested name %q", record.Name, name)
	}
	return record, nil
}

func decodeRecord(decoder *json.Decoder) (Record, error) {
	if err := requireObjectStart(decoder); err != nil {
		return Record{}, fmt.Errorf("session record: %w", err)
	}

	seen := map[string]bool{}
	var record Record
	var gotVersion, gotDomain, gotName, gotID, gotMode, gotState, gotBackend bool
	for decoder.More() {
		field, err := objectField(decoder, seen)
		if err != nil {
			return Record{}, fmt.Errorf("session record: %w", err)
		}
		switch field {
		case "version":
			err = decoder.Decode(&record.Version)
			gotVersion = true
		case "domain":
			var raw string
			err = decoder.Decode(&raw)
			if err == nil {
				record.Domain, err = domain.Parse(raw)
			}
			gotDomain = true
		case "name":
			var raw string
			err = decoder.Decode(&raw)
			if err == nil {
				record.Name, err = ParseName(raw)
			}
			gotName = true
		case "id":
			err = decoder.Decode(&record.ID)
			gotID = true
		case "mode":
			err = decoder.Decode(&record.Mode)
			gotMode = true
		case "intended_state":
			err = decoder.Decode(&record.IntendedState)
			gotState = true
		case "backend":
			record.Backend, err = decodeBackend(decoder)
			gotBackend = true
		case "golden_revision":
			err = decoder.Decode(&record.GoldenRevision)
		default:
			return Record{}, fmt.Errorf("unknown session record field %q", field)
		}
		if err != nil {
			return Record{}, fmt.Errorf("session record field %q: %w", field, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return Record{}, fmt.Errorf("session record: %w", err)
	}
	if !gotVersion || record.Version != recordVersion {
		return Record{}, fmt.Errorf("unsupported session record version %d", record.Version)
	}
	if !gotDomain || !gotName || !gotID || !gotMode || !gotState || !gotBackend {
		return Record{}, fmt.Errorf("session record has missing required fields")
	}
	if !validUUID(record.ID) {
		return Record{}, fmt.Errorf("invalid session record id")
	}
	if record.Mode != ModeClean && record.Mode != ModeQuarantine {
		return Record{}, fmt.Errorf("invalid session mode %q", record.Mode)
	}
	if !validState(record.IntendedState) {
		return Record{}, fmt.Errorf("invalid intended state %q", record.IntendedState)
	}
	return record, nil
}

func decodeBackend(decoder *json.Decoder) (BackendRef, error) {
	if err := requireObjectStart(decoder); err != nil {
		return BackendRef{}, err
	}
	seen := map[string]bool{}
	var backend BackendRef
	var gotKind, gotObjectID bool
	for decoder.More() {
		field, err := objectField(decoder, seen)
		if err != nil {
			return BackendRef{}, err
		}
		switch field {
		case "kind":
			err = decoder.Decode(&backend.Kind)
			gotKind = true
		case "object_id":
			err = decoder.Decode(&backend.ObjectID)
			gotObjectID = true
		default:
			return BackendRef{}, fmt.Errorf("unknown backend field %q", field)
		}
		if err != nil {
			return BackendRef{}, fmt.Errorf("backend field %q: %w", field, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return BackendRef{}, err
	}
	if !gotKind || !gotObjectID || backend.Kind != "tart" || !validBackendObjectID(backend.ObjectID) {
		return BackendRef{}, fmt.Errorf("invalid backend reference")
	}
	return backend, nil
}

func requirePrivateDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("must be a non-symlink directory")
	}
	if info.Mode().Perm()&0o007 != 0 {
		return fmt.Errorf("must not be accessible to group or other users")
	}
	return nil
}

func validState(state IntendedState) bool {
	switch state {
	case StateCreating, StateStopped, StateStarting, StateRunning, StateStopping, StateDeleting, StateFailed:
		return true
	default:
		return false
	}
}

func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for index, character := range raw {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func validBackendObjectID(raw string) bool {
	if len(raw) == 0 || len(raw) > 127 || !isAlphaNumeric(raw[0]) {
		return false
	}
	for index := 1; index < len(raw); index++ {
		if !isAlphaNumeric(raw[index]) && raw[index] != '-' {
			return false
		}
	}
	return true
}

func isAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}

func requireObjectStart(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("expected object")
	}
	return nil
}

func requireObjectEnd(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return fmt.Errorf("expected object end")
	}
	return nil
}

func objectField(decoder *json.Decoder, seen map[string]bool) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	field, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("expected object field")
	}
	if seen[field] {
		return "", fmt.Errorf("duplicate field %q", field)
	}
	seen[field] = true
	return field, nil
}
