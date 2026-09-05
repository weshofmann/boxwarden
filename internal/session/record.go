package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	recordVersionV1 = 1
	recordVersion   = 2

	maxReadinessDiagnosticBytes = 1024
)

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

// ReadinessStatus records the last durable lifecycle result. It is never
// evidence that a running backend is currently ready.
type ReadinessStatus string

const (
	ReadinessNotReady ReadinessStatus = "not_ready"
	ReadinessStarting ReadinessStatus = "starting"
	ReadinessReady    ReadinessStatus = "ready"
	ReadinessDrift    ReadinessStatus = "drift"
)

// ReadinessRecord is a bounded, non-secret audit hint written by lifecycle
// transitions. Callers must never put secrets in Diagnostic.
type ReadinessRecord struct {
	Status     ReadinessStatus `json:"status"`
	Diagnostic string          `json:"diagnostic"`
}

type Record struct {
	Version         int             `json:"version"`
	Domain          domain.ID       `json:"domain"`
	Name            Name            `json:"name"`
	ID              string          `json:"id"`
	Mode            Mode            `json:"mode"`
	IntendedState   IntendedState   `json:"intended_state"`
	Backend         BackendRef      `json:"backend"`
	GoldenRevision  string          `json:"golden_revision,omitempty"`
	StartGeneration string          `json:"start_generation,omitempty"`
	Readiness       ReadinessRecord `json:"readiness"`
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
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return Record{}, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	sessions, err := openSessionChild(root, "sessions", false)
	if err != nil {
		return Record{}, fmt.Errorf("session directory: %w", err)
	}
	defer sessions.Close()
	return loadRecordFromRoot(sessions, domainID, name)
}

func loadRecordFromRoot(sessions *os.Root, domainID domain.ID, name Name) (Record, error) {
	file, err := openSessionPrivateRegular(sessions, string(name)+".json")
	if err != nil {
		return Record{}, fmt.Errorf("open session record %q: %w", name, err)
	}
	defer file.Close()

	contents, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return Record{}, fmt.Errorf("read session record %q: %w", name, err)
	}
	if len(contents) > 1<<20 {
		return Record{}, fmt.Errorf("session record %q exceeds 1 MiB", name)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
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
	var gotVersion, gotDomain, gotName, gotID, gotMode, gotState, gotBackend, gotGoldenRevision, gotGeneration, gotReadiness bool
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
			gotGoldenRevision = true
		case "start_generation":
			err = decoder.Decode(&record.StartGeneration)
			gotGeneration = true
		case "readiness":
			record.Readiness, err = decodeReadiness(decoder)
			gotReadiness = true
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
	if !gotVersion || (record.Version != recordVersionV1 && record.Version != recordVersion) {
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
	switch record.Version {
	case recordVersionV1:
		if gotGeneration || gotReadiness {
			return Record{}, fmt.Errorf("version 1 session record has version 2 fields")
		}
	case recordVersion:
		if !gotGoldenRevision || !validBackendObjectID(record.GoldenRevision) || !gotReadiness {
			return Record{}, fmt.Errorf("session record has missing required fields")
		}
		if err := validateStartGeneration(record); err != nil {
			return Record{}, err
		}
		if err := validateReadiness(record); err != nil {
			return Record{}, err
		}
	}
	return record, nil
}

func validateStartGeneration(record Record) error {
	switch record.IntendedState {
	case StateStarting, StateRunning:
		if !validUUID(record.StartGeneration) {
			return fmt.Errorf("%s record requires a valid start generation", record.IntendedState)
		}
	default:
		if record.StartGeneration != "" {
			return fmt.Errorf("%s record must not have a start generation", record.IntendedState)
		}
	}
	return nil
}

func decodeReadiness(decoder *json.Decoder) (ReadinessRecord, error) {
	if err := requireObjectStart(decoder); err != nil {
		return ReadinessRecord{}, err
	}
	seen := map[string]bool{}
	var readiness ReadinessRecord
	var gotStatus, gotDiagnostic bool
	for decoder.More() {
		field, err := objectField(decoder, seen)
		if err != nil {
			return ReadinessRecord{}, err
		}
		switch field {
		case "status":
			err = decoder.Decode(&readiness.Status)
			gotStatus = true
		case "diagnostic":
			err = decoder.Decode(&readiness.Diagnostic)
			gotDiagnostic = true
		default:
			return ReadinessRecord{}, fmt.Errorf("unknown readiness field %q", field)
		}
		if err != nil {
			return ReadinessRecord{}, fmt.Errorf("readiness field %q: %w", field, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return ReadinessRecord{}, err
	}
	if !gotStatus || !gotDiagnostic {
		return ReadinessRecord{}, fmt.Errorf("readiness has missing required fields")
	}
	return readiness, nil
}

func validateReadiness(record Record) error {
	if len(record.Readiness.Diagnostic) > maxReadinessDiagnosticBytes {
		return fmt.Errorf("readiness diagnostic exceeds %d bytes", maxReadinessDiagnosticBytes)
	}
	switch record.Readiness.Status {
	case ReadinessNotReady:
		if record.StartGeneration != "" {
			return fmt.Errorf("not-ready record must not have a start generation")
		}
	case ReadinessStarting:
		if record.IntendedState != StateStarting {
			return fmt.Errorf("starting record requires starting intent")
		}
	case ReadinessReady:
		if record.IntendedState != StateRunning {
			return fmt.Errorf("ready record requires running intent")
		}
	case ReadinessDrift:
		if record.IntendedState != StateRunning {
			return fmt.Errorf("drift record requires running intent")
		}
	default:
		return fmt.Errorf("invalid readiness status %q", record.Readiness.Status)
	}
	return nil
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
