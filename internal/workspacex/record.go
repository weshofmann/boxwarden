// Package workspacex stores domain-owned workspace volume bindings.
//
// A record is keyed by the stable volume UUID. It is the durable attachment
// authority; an advisory lock only serializes a live operation and never proves
// that a backend stopped after a supervisor exit.
package workspacex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const recordVersion = 1

const FormatRawExt4 = "raw-ext4"

type State string

const (
	StateCreating  State = "creating"
	StateAvailable State = "available"
	StateFailed    State = "failed"
)

// DiskIdentity binds a raw file to its exact host filesystem object.
// SizeBytes remains in the enclosing record so a changed file length fails
// admission without accepting a replacement inode.
type DiskIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// Use records the backend that may still have this volume attached. Clearing
// it requires backend-observed stop/wait/reap, not a released process lock.
type Use struct {
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
	Generation    string `json:"generation"`
}

// Attachment names one stable sandbox identity and one fixed guest mount.
// SessionName is retained for exact session-lock acquisition; rebuild may
// change the system backend object without changing this binding.
type Attachment struct {
	SessionID   string `json:"session_id"`
	SessionName string `json:"session_name"`
	MountPath   string `json:"mount_path"`
}

// Pending is a durable incomplete operation marker. An unresolved marker
// blocks attachment and disk use until the specific operation is reconciled.
type Pending struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type Record struct {
	Version        int           `json:"version"`
	Domain         domain.ID     `json:"domain"`
	VolumeID       string        `json:"volume_id"`
	SizeBytes      int64         `json:"size_bytes"`
	Format         string        `json:"format"`
	FilesystemUUID string        `json:"filesystem_uuid"`
	State          State         `json:"state"`
	Disk           *DiskIdentity `json:"disk,omitempty"`
	Attachment     *Attachment   `json:"attachment,omitempty"`
	Use            *Use          `json:"use,omitempty"`
	Pending        *Pending      `json:"pending,omitempty"`
}

func validateRecord(expectedDomain domain.ID, record Record) error {
	parsed, err := domain.Parse(string(expectedDomain))
	if err != nil {
		return err
	}
	if record.Version != recordVersion {
		return fmt.Errorf("unsupported workspace record version %d", record.Version)
	}
	if record.Domain != parsed {
		return fmt.Errorf("workspace record domain %q does not match %q", record.Domain, parsed)
	}
	if !validUUID(record.VolumeID) || !validUUID(record.FilesystemUUID) || record.Format != FormatRawExt4 {
		return fmt.Errorf("invalid workspace volume or filesystem identity")
	}
	if record.SizeBytes < 4096 || record.SizeBytes > 1<<43 || record.SizeBytes%512 != 0 {
		return fmt.Errorf("invalid workspace disk size %d", record.SizeBytes)
	}
	switch record.State {
	case StateCreating:
		if record.Disk != nil || record.Use != nil || record.Attachment != nil {
			return fmt.Errorf("creating workspace must not claim a disk, attachment, or backend use")
		}
	case StateAvailable:
		if record.Disk == nil || record.Disk.Device == 0 || record.Disk.Inode == 0 {
			return fmt.Errorf("available workspace requires an exact disk identity")
		}
	case StateFailed:
		if record.Use != nil || record.Attachment != nil {
			return fmt.Errorf("failed workspace must not claim attachment or backend use")
		}
		if record.Disk != nil && (record.Disk.Device == 0 || record.Disk.Inode == 0) {
			return fmt.Errorf("failed workspace has incomplete disk identity")
		}
	default:
		return fmt.Errorf("invalid workspace state %q", record.State)
	}
	if record.Use != nil && (record.Use.BackendKind != "tart" || !validObjectID(record.Use.BackendObject) || !validUUID(record.Use.Generation)) {
		return fmt.Errorf("invalid workspace backend use reservation")
	}
	if record.Attachment != nil {
		if record.State != StateAvailable || !validUUID(record.Attachment.SessionID) || !validSessionName(record.Attachment.SessionName) || !validMountPath(record.Attachment.MountPath) {
			return fmt.Errorf("invalid workspace attachment")
		}
	}
	if record.Use != nil && (record.Attachment == nil || record.Pending != nil) {
		return fmt.Errorf("workspace use requires a resolved attachment")
	}
	if record.Pending != nil {
		if !validUUID(record.Pending.ID) || !validPendingKind(record.Pending.Kind) {
			return fmt.Errorf("invalid pending workspace operation")
		}
		if record.Pending.Kind == "export-snapshot" && (record.State != StateAvailable || record.Attachment == nil || record.Disk == nil) {
			return fmt.Errorf("export snapshot requires an available attached workspace")
		}
	}
	return nil
}

func validSessionName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if (name[i] < 'a' || name[i] > 'z') && (name[i] < '0' || name[i] > '9') {
			return false
		}
	}
	return true
}

func validMountPath(mount string) bool {
	const prefix = "/home/boxwarden/workspaces/"
	if len(mount) <= len(prefix) || len(mount) > 255 || mount[:len(prefix)] != prefix {
		return false
	}
	name := mount[len(prefix):]
	return validSessionName(name)
}

func validPendingKind(kind string) bool {
	switch kind {
	case "create", "format", "import", "attach", "detach", "export-snapshot":
		return true
	default:
		return false
	}
}

func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i := 0; i < len(raw); i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if raw[i] != '-' {
				return false
			}
			continue
		}
		if (raw[i] < '0' || raw[i] > '9') && (raw[i] < 'a' || raw[i] > 'f') {
			return false
		}
	}
	return true
}

func validObjectID(raw string) bool {
	if len(raw) == 0 || len(raw) > 127 || !alphaNumeric(raw[0]) {
		return false
	}
	for i := 1; i < len(raw); i++ {
		if !alphaNumeric(raw[i]) && raw[i] != '-' {
			return false
		}
	}
	return true
}

func alphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

func decodeRecord(raw []byte, expectedDomain domain.ID) (Record, error) {
	var record Record
	seen, err := decodeObject(raw, map[string]func(json.RawMessage) error{
		"version":         func(v json.RawMessage) error { return json.Unmarshal(v, &record.Version) },
		"domain":          func(v json.RawMessage) error { return json.Unmarshal(v, &record.Domain) },
		"volume_id":       func(v json.RawMessage) error { return json.Unmarshal(v, &record.VolumeID) },
		"size_bytes":      func(v json.RawMessage) error { return json.Unmarshal(v, &record.SizeBytes) },
		"format":          func(v json.RawMessage) error { return json.Unmarshal(v, &record.Format) },
		"filesystem_uuid": func(v json.RawMessage) error { return json.Unmarshal(v, &record.FilesystemUUID) },
		"state":           func(v json.RawMessage) error { return json.Unmarshal(v, &record.State) },
		"disk": func(v json.RawMessage) error {
			var disk DiskIdentity
			fields, err := decodeObject(v, map[string]func(json.RawMessage) error{
				"device": func(v json.RawMessage) error { return json.Unmarshal(v, &disk.Device) },
				"inode":  func(v json.RawMessage) error { return json.Unmarshal(v, &disk.Inode) },
			})
			if err != nil {
				return err
			}
			if !fields["device"] || !fields["inode"] {
				return fmt.Errorf("disk identity missing field")
			}
			record.Disk = &disk
			return nil
		},
		"use": func(v json.RawMessage) error {
			var use Use
			fields, err := decodeObject(v, map[string]func(json.RawMessage) error{
				"backend_kind":   func(v json.RawMessage) error { return json.Unmarshal(v, &use.BackendKind) },
				"backend_object": func(v json.RawMessage) error { return json.Unmarshal(v, &use.BackendObject) },
				"generation":     func(v json.RawMessage) error { return json.Unmarshal(v, &use.Generation) },
			})
			if err != nil {
				return err
			}
			if !fields["backend_kind"] || !fields["backend_object"] || !fields["generation"] {
				return fmt.Errorf("use reservation missing field")
			}
			record.Use = &use
			return nil
		},
		"attachment": func(v json.RawMessage) error {
			var attachment Attachment
			fields, err := decodeObject(v, map[string]func(json.RawMessage) error{
				"session_id":   func(v json.RawMessage) error { return json.Unmarshal(v, &attachment.SessionID) },
				"session_name": func(v json.RawMessage) error { return json.Unmarshal(v, &attachment.SessionName) },
				"mount_path":   func(v json.RawMessage) error { return json.Unmarshal(v, &attachment.MountPath) },
			})
			if err != nil {
				return err
			}
			if !fields["session_id"] || !fields["session_name"] || !fields["mount_path"] {
				return fmt.Errorf("attachment missing field")
			}
			record.Attachment = &attachment
			return nil
		},
		"pending": func(v json.RawMessage) error {
			var pending Pending
			fields, err := decodeObject(v, map[string]func(json.RawMessage) error{
				"kind": func(v json.RawMessage) error { return json.Unmarshal(v, &pending.Kind) },
				"id":   func(v json.RawMessage) error { return json.Unmarshal(v, &pending.ID) },
			})
			if err != nil {
				return err
			}
			if !fields["kind"] || !fields["id"] {
				return fmt.Errorf("pending operation missing field")
			}
			record.Pending = &pending
			return nil
		},
	})
	if err != nil {
		return Record{}, fmt.Errorf("workspace record: %w", err)
	}
	for _, name := range []string{"version", "domain", "volume_id", "size_bytes", "format", "filesystem_uuid", "state"} {
		if !seen[name] {
			return Record{}, fmt.Errorf("workspace record missing %q", name)
		}
	}
	if err := validateRecord(expectedDomain, record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func decodeObject(raw []byte, handlers map[string]func(json.RawMessage) error) (map[string]bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if start != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	seen := make(map[string]bool, len(handlers))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("expected object field")
		}
		if seen[name] {
			return nil, fmt.Errorf("duplicate field %q", name)
		}
		seen[name] = true
		handler, ok := handlers[name]
		if !ok {
			return nil, fmt.Errorf("unknown field %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
		if err := handler(value); err != nil {
			return nil, fmt.Errorf("field %q: %w", name, err)
		}
	}
	end, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if end != json.Delim('}') {
		return nil, fmt.Errorf("expected object end")
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("trailing JSON value")
	}
	return seen, nil
}
