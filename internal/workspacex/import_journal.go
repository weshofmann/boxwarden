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
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/importx"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/renamex"
)

const maxImportJournalBytes = 8 << 10

var syncImportDirectory = syncDirectory

type ImportPhase string

const (
	ImportCaptured     ImportPhase = "captured"
	ImportTransferring ImportPhase = "transferring"
)

// ImportJournal binds one bounded host snapshot to the exact live guest and
// mounted workspace generation. A transferring record is intentionally visible
// after interruption; it never constitutes a verified import.
type ImportJournal struct {
	Version        int         `json:"version"`
	ID             string      `json:"id"`
	Domain         domain.ID   `json:"domain"`
	SessionID      string      `json:"session_id"`
	SessionName    string      `json:"session_name"`
	BackendObject  string      `json:"backend_object"`
	Generation     string      `json:"generation"`
	VolumeID       string      `json:"volume_id"`
	FilesystemUUID string      `json:"filesystem_uuid"`
	MountPath      string      `json:"mount_path"`
	SourceDigest   string      `json:"source_digest"`
	FileCount      int         `json:"file_count"`
	TotalBytes     int64       `json:"total_bytes"`
	Phase          ImportPhase `json:"phase"`
}

func validateImportJournal(j ImportJournal) error {
	if j.Version != 1 || !validUUID(j.ID) || j.ID == "00000000-0000-0000-0000-000000000000" ||
		!validUUID(j.SessionID) || !validSessionName(j.SessionName) || !validObjectID(j.BackendObject) ||
		!validUUID(j.Generation) || !validUUID(j.VolumeID) || !validUUID(j.FilesystemUUID) ||
		!validMountPath(j.MountPath) || !validSHA256(j.SourceDigest) || j.FileCount < 1 || j.FileCount > 256 ||
		j.TotalBytes < 0 || j.TotalBytes > 16<<20 {
		return fmt.Errorf("invalid import transaction binding")
	}
	if _, err := domain.Parse(string(j.Domain)); err != nil {
		return err
	}
	if j.Phase != ImportCaptured && j.Phase != ImportTransferring {
		return fmt.Errorf("invalid import phase")
	}
	return nil
}

func admitImportSnapshot(stateRoot string, j ImportJournal) error {
	snapshot, err := importx.InspectSnapshot(filepath.Join(stateRoot, "imports"), j.ID)
	if err != nil {
		return err
	}
	if snapshot.Digest != j.SourceDigest || snapshot.FileCount != j.FileCount || snapshot.TotalBytes != j.TotalBytes {
		return fmt.Errorf("captured import bytes differ from journal")
	}
	return nil
}

// createImportJournal is a storage primitive. BeginImport owns the live
// session, workspace Use, and mount-readiness admission before invoking it.
func createImportJournal(stateRoot string, journal ImportJournal) error {
	if journal.Phase != ImportCaptured {
		return fmt.Errorf("new import must begin captured")
	}
	if err := validateImportJournal(journal); err != nil {
		return err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	imports, err := openChild(root, "imports", false)
	if err != nil {
		return err
	}
	defer imports.Close()
	if err := admitImportSnapshot(stateRoot, journal); err != nil {
		return err
	}
	if current, err := loadImportJournalFromRoot(imports, journal.Domain, journal.ID); err == nil {
		if current != journal {
			return fmt.Errorf("existing import journal differs from captured transaction")
		}
		return syncImportDirectory(imports)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := encodeImportJournal(journal)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := journal.ID + ".json.tmp-" + hex.EncodeToString(nonce[:])
	file, err := imports.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer imports.Remove(temporary)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := renamex.NoReplace(imports, temporary, journal.ID+".json"); err != nil {
		return err
	}
	if err := syncImportDirectory(imports); err != nil {
		return fmt.Errorf("import journal published but parent sync uncertain: %w", err)
	}
	return nil
}

func loadImportJournal(stateRoot string, expectedDomain domain.ID, id string) (ImportJournal, error) {
	if !validUUID(id) {
		return ImportJournal{}, fmt.Errorf("invalid import transaction ID")
	}
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return ImportJournal{}, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return ImportJournal{}, err
	}
	defer root.Close()
	imports, err := openChild(root, "imports", false)
	if err != nil {
		return ImportJournal{}, err
	}
	defer imports.Close()
	return loadImportJournalFromRoot(imports, expectedDomain, id)
}

// LoadImportJournal strictly reads one domain-bound import transaction. The
// caller must separately admit the current source and live workspace binding.
func LoadImportJournal(stateRoot string, expectedDomain domain.ID, id string) (ImportJournal, error) {
	return loadImportJournal(stateRoot, expectedDomain, id)
}

func loadImportJournalFromRoot(imports *os.Root, expectedDomain domain.ID, id string) (ImportJournal, error) {
	file, err := openPrivateFile(imports, id+".json")
	if err != nil {
		return ImportJournal{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxImportJournalBytes+1))
	if err != nil || len(raw) > maxImportJournalBytes {
		return ImportJournal{}, fmt.Errorf("read bounded import journal: %v", err)
	}
	j, err := decodeImportJournal(raw)
	if err != nil {
		return ImportJournal{}, err
	}
	if j.Domain != expectedDomain || j.ID != id {
		return ImportJournal{}, fmt.Errorf("import journal differs from requested domain or transaction")
	}
	return j, nil
}

func decodeImportJournal(raw []byte) (ImportJournal, error) {
	var j ImportJournal
	seen, err := decodeObject(raw, map[string]func(json.RawMessage) error{
		"version":         func(v json.RawMessage) error { return json.Unmarshal(v, &j.Version) },
		"id":              func(v json.RawMessage) error { return json.Unmarshal(v, &j.ID) },
		"domain":          func(v json.RawMessage) error { return json.Unmarshal(v, &j.Domain) },
		"session_id":      func(v json.RawMessage) error { return json.Unmarshal(v, &j.SessionID) },
		"session_name":    func(v json.RawMessage) error { return json.Unmarshal(v, &j.SessionName) },
		"backend_object":  func(v json.RawMessage) error { return json.Unmarshal(v, &j.BackendObject) },
		"generation":      func(v json.RawMessage) error { return json.Unmarshal(v, &j.Generation) },
		"volume_id":       func(v json.RawMessage) error { return json.Unmarshal(v, &j.VolumeID) },
		"filesystem_uuid": func(v json.RawMessage) error { return json.Unmarshal(v, &j.FilesystemUUID) },
		"mount_path":      func(v json.RawMessage) error { return json.Unmarshal(v, &j.MountPath) },
		"source_digest":   func(v json.RawMessage) error { return json.Unmarshal(v, &j.SourceDigest) },
		"file_count":      func(v json.RawMessage) error { return json.Unmarshal(v, &j.FileCount) },
		"total_bytes":     func(v json.RawMessage) error { return json.Unmarshal(v, &j.TotalBytes) },
		"phase":           func(v json.RawMessage) error { return json.Unmarshal(v, &j.Phase) },
	})
	if err != nil {
		return ImportJournal{}, err
	}
	for _, required := range []string{"version", "id", "domain", "session_id", "session_name", "backend_object", "generation", "volume_id", "filesystem_uuid", "mount_path", "source_digest", "file_count", "total_bytes", "phase"} {
		if !seen[required] {
			return ImportJournal{}, fmt.Errorf("import journal missing %q", required)
		}
	}
	if err := validateImportJournal(j); err != nil {
		return ImportJournal{}, err
	}
	return j, nil
}

func encodeImportJournal(j ImportJournal) ([]byte, error) {
	raw, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	raw = append(raw, '\n')
	if len(raw) > maxImportJournalBytes {
		return nil, fmt.Errorf("import journal exceeds limit")
	}
	return raw, nil
}

// advanceImportJournal persists only captured->transferring. The transfer
// owner must recheck live bindings under the transition lock first; this
// storage helper cannot assert a guest import or its completion.
func advanceImportJournal(ctx context.Context, stateRoot string, expected, next ImportJournal) error {
	if err := validateImportJournal(expected); err != nil {
		return err
	}
	if err := validateImportJournal(next); err != nil {
		return err
	}
	oldBinding, newBinding := expected, next
	oldBinding.Phase, newBinding.Phase = "", ""
	if oldBinding != newBinding || expected.Phase != ImportCaptured || next.Phase != ImportTransferring {
		return fmt.Errorf("invalid import journal transition")
	}
	held, err := lock.Acquire(ctx, stateRoot, "import-"+string(expected.Domain)+"-"+expected.ID)
	if err != nil {
		return err
	}
	defer held.Release()
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	imports, err := openChild(root, "imports", false)
	if err != nil {
		return err
	}
	defer imports.Close()
	current, err := loadImportJournalFromRoot(imports, expected.Domain, expected.ID)
	if err != nil {
		return fmt.Errorf("import journal changed before transition: %v", err)
	}
	if err := admitImportSnapshot(stateRoot, expected); err != nil {
		return err
	}
	if current == next {
		return syncImportDirectory(imports) // retry after an uncertain final directory sync
	}
	if current != expected {
		return fmt.Errorf("import journal changed before transition")
	}
	raw, err := encodeImportJournal(next)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := next.ID + ".json.tmp-" + hex.EncodeToString(nonce[:])
	file, err := imports.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer imports.Remove(temporary)
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err := imports.Rename(temporary, next.ID+".json"); err != nil {
		return err
	}
	if err := syncImportDirectory(imports); err != nil {
		return fmt.Errorf("import journal advanced but parent sync uncertain: %w", err)
	}
	return nil
}
