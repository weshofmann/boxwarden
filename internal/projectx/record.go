package projectx

import (
	"fmt"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

func validateKey(d domain.ID, name string) error {
	if _, err := domain.Parse(string(d)); err != nil {
		return err
	}
	_, err := session.ParseName(name)
	return err
}
func validateRecord(d domain.ID, r Record) error {
	if err := validateKey(d, r.Name); err != nil {
		return err
	}
	if r.Version != 1 || r.Domain != d {
		return fmt.Errorf("invalid project version or domain")
	}
	if err := backend.ValidateObjectID(r.Base); err != nil {
		return fmt.Errorf("project base: %w", err)
	}
	// The current workspace export cap is 1 GiB; projects must be exportable.
	if !validUUID(r.VolumeID) || !validUUID(r.FilesystemUUID) || r.SizeBytes < 16<<20 || r.SizeBytes > 1<<30 || r.SizeBytes%512 != 0 {
		return fmt.Errorf("invalid project volume identity or size")
	}
	if (r.SessionID == "") != (r.BackendObject == "") {
		return fmt.Errorf("incomplete project creation receipt")
	}
	if r.SessionID != "" {
		if !validUUID(r.SessionID) {
			return fmt.Errorf("invalid project session ID")
		}
		if err := backend.ValidateObjectID(r.BackendObject); err != nil {
			return err
		}
	}
	if r.Initialized && r.SessionID == "" {
		return fmt.Errorf("initialized project lacks creation receipt")
	}
	if (r.ImportID == "") != (r.ImportSource == "") {
		return fmt.Errorf("incomplete project import intent")
	}
	if r.ImportID != "" && (!validUUID(r.ImportID) || !canonicalPath(r.ImportSource)) {
		return fmt.Errorf("invalid project import ID or source locator")
	}
	if r.Imported && (r.ImportID == "" || !r.Initialized) {
		return fmt.Errorf("imported project lacks import or initialization receipt")
	}
	return nil
}

// Match the canonical lowercase UUID convention of session/workspacex.
func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i := range len(raw) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if raw[i] != '-' {
				return false
			}
		} else if (raw[i] < '0' || raw[i] > '9') && (raw[i] < 'a' || raw[i] > 'f') {
			return false
		}
	}
	return true
}
