package workspacex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const maxExportJournalBytes = 64 << 10
const maxAlphaExportVolumeBytes = 1 << 30

type ExportPhase string

const (
	ExportCopying       ExportPhase = "copying"
	ExportSnapshotReady ExportPhase = "snapshot-ready"
	ExportInspected     ExportPhase = "inspected"
	ExportPublished     ExportPhase = "published"
)

type ExportSnapshot struct {
	Identity DiskIdentity `json:"identity"`
	SHA256   string       `json:"sha256"`
}

// ExportJournal remains authoritative after the volume's copy Pending marker
// clears. The final destination name is the transaction UUID without dashes,
// as used by exportx.Receive; no caller-selected final name is stored.
type ExportJournal struct {
	Version           int             `json:"version"`
	ID                string          `json:"id"`
	Domain            domain.ID       `json:"domain"`
	VolumeID          string          `json:"volume_id"`
	SessionID         string          `json:"session_id"`
	SessionName       string          `json:"session_name"`
	BackendObject     string          `json:"backend_object"`
	FilesystemUUID    string          `json:"filesystem_uuid"`
	SizeBytes         int64           `json:"size_bytes"`
	Source            DiskIdentity    `json:"source"`
	SnapshotPath      string          `json:"snapshot_path"`
	DestinationParent string          `json:"destination_parent"`
	Destination       DiskIdentity    `json:"destination"`
	Selected          []string        `json:"selected"`
	Phase             ExportPhase     `json:"phase"`
	Snapshot          *ExportSnapshot `json:"snapshot,omitempty"`
}

func validateExportJournal(j ExportJournal) error {
	if j.Version != 1 || !validUUID(j.ID) || j.ID == "00000000-0000-0000-0000-000000000000" || !validUUID(j.VolumeID) || !validUUID(j.SessionID) ||
		!validSessionName(j.SessionName) || !validObjectID(j.BackendObject) || !validUUID(j.FilesystemUUID) ||
		j.SizeBytes < 4096 || j.SizeBytes > maxAlphaExportVolumeBytes || j.SizeBytes%512 != 0 ||
		j.Source.Device == 0 || j.Source.Inode == 0 || j.Destination.Device == 0 || j.Destination.Inode == 0 ||
		j.SnapshotPath != filepath.Join("exports", j.ID, "snapshot.raw") ||
		!filepath.IsAbs(j.DestinationParent) || filepath.Clean(j.DestinationParent) != j.DestinationParent {
		return fmt.Errorf("invalid export transaction binding")
	}
	if _, err := domain.Parse(string(j.Domain)); err != nil {
		return err
	}
	if len(j.Selected) == 0 || len(j.Selected) > 1024 {
		return fmt.Errorf("invalid export selection count")
	}
	seen := make(map[string]bool, len(j.Selected))
	for _, selected := range j.Selected {
		if !validExportSelection(selected) || seen[strings.ToLower(selected)] {
			return fmt.Errorf("invalid or colliding export selection")
		}
		seen[strings.ToLower(selected)] = true
	}
	switch j.Phase {
	case ExportCopying:
		if j.Snapshot != nil {
			return fmt.Errorf("copying export cannot claim a snapshot")
		}
	case ExportSnapshotReady, ExportInspected, ExportPublished:
		if j.Snapshot == nil || j.Snapshot.Identity.Device == 0 || j.Snapshot.Identity.Inode == 0 ||
			j.Snapshot.Identity == j.Source || !validSHA256(j.Snapshot.SHA256) {
			return fmt.Errorf("export lacks exact private snapshot identity")
		}
	default:
		return fmt.Errorf("invalid export phase")
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' && value[i] < 'a' || value[i] > 'f' {
			return false
		}
	}
	return true
}

func validExportSelection(value string) bool {
	// Match exportx's wire-path envelope before a selection is persisted.
	if len(value) == 0 || len(value) > 512 || strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") {
		return false
	}
	components := strings.Split(value, "/")
	if len(components) > 16 {
		return false
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > 64 {
			return false
		}
		for i := range component {
			c := component[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
				return false
			}
		}
	}
	return true
}

func createExportJournal(stateRoot string, journal ExportJournal) error {
	if journal.Phase != ExportCopying {
		return fmt.Errorf("new export must begin copying")
	}
	if err := validateExportJournal(journal); err != nil {
		return err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", true)
	if err != nil {
		return err
	}
	defer exports.Close()
	raw, err := encodeExportJournal(journal)
	if err != nil {
		return err
	}
	name := journal.ID + ".json"
	file, err := exports.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err // an incomplete journal remains for explicit reconciliation
	}
	return syncDirectory(exports)
}

func loadExportJournal(stateRoot string, expectedDomain domain.ID, id string) (ExportJournal, error) {
	if !validUUID(id) {
		return ExportJournal{}, fmt.Errorf("invalid export transaction ID")
	}
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return ExportJournal{}, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return ExportJournal{}, err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return ExportJournal{}, err
	}
	defer exports.Close()
	return loadExportJournalFromRoot(exports, expectedDomain, id)
}

func loadExportJournalFromRoot(exports *os.Root, expectedDomain domain.ID, id string) (ExportJournal, error) {
	file, err := openPrivateFile(exports, id+".json")
	if err != nil {
		return ExportJournal{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxExportJournalBytes+1))
	if err != nil || len(raw) > maxExportJournalBytes {
		return ExportJournal{}, fmt.Errorf("read bounded export journal: %v", err)
	}
	journal, err := decodeExportJournal(raw)
	if err != nil {
		return ExportJournal{}, err
	}
	if journal.ID != id || journal.Domain != expectedDomain {
		return ExportJournal{}, fmt.Errorf("export journal identity differs from requested domain or transaction")
	}
	return journal, nil
}

func decodeExportJournal(raw []byte) (ExportJournal, error) {
	var j ExportJournal
	fields, err := decodeObject(raw, map[string]func(json.RawMessage) error{
		"version":            func(v json.RawMessage) error { return json.Unmarshal(v, &j.Version) },
		"id":                 func(v json.RawMessage) error { return json.Unmarshal(v, &j.ID) },
		"domain":             func(v json.RawMessage) error { return json.Unmarshal(v, &j.Domain) },
		"volume_id":          func(v json.RawMessage) error { return json.Unmarshal(v, &j.VolumeID) },
		"session_id":         func(v json.RawMessage) error { return json.Unmarshal(v, &j.SessionID) },
		"session_name":       func(v json.RawMessage) error { return json.Unmarshal(v, &j.SessionName) },
		"backend_object":     func(v json.RawMessage) error { return json.Unmarshal(v, &j.BackendObject) },
		"filesystem_uuid":    func(v json.RawMessage) error { return json.Unmarshal(v, &j.FilesystemUUID) },
		"size_bytes":         func(v json.RawMessage) error { return json.Unmarshal(v, &j.SizeBytes) },
		"source":             func(v json.RawMessage) error { return decodeDiskIdentity(v, &j.Source) },
		"snapshot_path":      func(v json.RawMessage) error { return json.Unmarshal(v, &j.SnapshotPath) },
		"destination_parent": func(v json.RawMessage) error { return json.Unmarshal(v, &j.DestinationParent) },
		"destination":        func(v json.RawMessage) error { return decodeDiskIdentity(v, &j.Destination) },
		"selected":           func(v json.RawMessage) error { return json.Unmarshal(v, &j.Selected) },
		"phase":              func(v json.RawMessage) error { return json.Unmarshal(v, &j.Phase) },
		"snapshot": func(v json.RawMessage) error {
			var snapshot ExportSnapshot
			seen, err := decodeObject(v, map[string]func(json.RawMessage) error{
				"identity": func(v json.RawMessage) error { return decodeDiskIdentity(v, &snapshot.Identity) },
				"sha256":   func(v json.RawMessage) error { return json.Unmarshal(v, &snapshot.SHA256) },
			})
			if err != nil || !seen["identity"] || !seen["sha256"] {
				return fmt.Errorf("invalid snapshot fields: %v", err)
			}
			j.Snapshot = &snapshot
			return nil
		},
	})
	if err != nil {
		return ExportJournal{}, err
	}
	for _, required := range []string{"version", "id", "domain", "volume_id", "session_id", "session_name", "backend_object", "filesystem_uuid", "size_bytes", "source", "snapshot_path", "destination_parent", "destination", "selected", "phase"} {
		if !fields[required] {
			return ExportJournal{}, fmt.Errorf("export journal missing %q", required)
		}
	}
	if err := validateExportJournal(j); err != nil {
		return ExportJournal{}, err
	}
	return j, nil
}

func decodeDiskIdentity(raw json.RawMessage, identity *DiskIdentity) error {
	seen, err := decodeObject(raw, map[string]func(json.RawMessage) error{
		"device": func(v json.RawMessage) error { return json.Unmarshal(v, &identity.Device) },
		"inode":  func(v json.RawMessage) error { return json.Unmarshal(v, &identity.Inode) },
	})
	if err != nil || !seen["device"] || !seen["inode"] {
		return fmt.Errorf("invalid disk identity fields: %v", err)
	}
	return nil
}

func advanceExportJournal(ctx context.Context, stateRoot string, expected, next ExportJournal) error {
	if err := validateExportJournal(expected); err != nil {
		return err
	}
	if err := validateExportJournal(next); err != nil {
		return err
	}
	if next.ID != expected.ID || next.Domain != expected.Domain || next.Version != expected.Version ||
		next.VolumeID != expected.VolumeID || next.SessionID != expected.SessionID || next.SessionName != expected.SessionName ||
		next.BackendObject != expected.BackendObject || next.FilesystemUUID != expected.FilesystemUUID || next.SizeBytes != expected.SizeBytes ||
		next.Source != expected.Source || next.SnapshotPath != expected.SnapshotPath || next.DestinationParent != expected.DestinationParent || next.Destination != expected.Destination ||
		!reflect.DeepEqual(next.Selected, expected.Selected) || !validExportPhaseAdvance(expected, next) {
		return fmt.Errorf("invalid export journal transition")
	}
	held, err := lock.Acquire(ctx, stateRoot, "export-"+string(expected.Domain)+"-"+expected.ID)
	if err != nil {
		return err
	}
	defer held.Release()
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if err != nil {
		return err
	}
	defer exports.Close()
	current, err := loadExportJournalFromRoot(exports, expected.Domain, expected.ID)
	if err != nil || !reflect.DeepEqual(current, expected) {
		return fmt.Errorf("export journal changed before transition: %v", err)
	}
	raw, err := encodeExportJournal(next)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	tempName := next.ID + ".json.tmp-" + hex.EncodeToString(nonce[:])
	temp, err := exports.OpenFile(tempName, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer exports.Remove(tempName)
	if _, err = temp.Write(raw); err == nil {
		err = temp.Sync()
	}
	err = errors.Join(err, temp.Close())
	if err != nil {
		return err
	}
	if err := exports.Rename(tempName, next.ID+".json"); err != nil {
		return err
	}
	return syncDirectory(exports)
}

func encodeExportJournal(journal ExportJournal) ([]byte, error) {
	raw, err := json.Marshal(journal)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > maxExportJournalBytes {
		return nil, fmt.Errorf("export journal exceeds limit")
	}
	return raw, nil
}

func validExportPhaseAdvance(old, next ExportJournal) bool {
	switch old.Phase {
	case ExportCopying:
		return next.Phase == ExportSnapshotReady && next.Snapshot != nil
	case ExportSnapshotReady:
		return next.Phase == ExportInspected && reflect.DeepEqual(next.Snapshot, old.Snapshot)
	case ExportInspected:
		return next.Phase == ExportPublished && reflect.DeepEqual(next.Snapshot, old.Snapshot)
	default:
		return false
	}
}
