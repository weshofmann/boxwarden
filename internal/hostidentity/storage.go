package hostidentity

import (
	"errors"
	"fmt"
	"path/filepath"
)

// ErrConfigLocationInadmissible identifies configuration storage/ownership
// defects separately from unavailable workspace backing storage.
var ErrConfigLocationInadmissible = errors.New("configuration location inadmissible")

// StorageExpectation is a trusted config declaration, kept on a different
// filesystem from the workspace backing volume.
type StorageExpectation struct {
	ConfigPath string
	StateRoot  string
	MountPoint string
	VolumeUUID string
}

func (s StorageExpectation) Validate() error {
	for _, path := range []string{s.ConfigPath, s.StateRoot, s.MountPoint} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("workspace storage path must be clean and absolute")
		}
	}
	if len(s.VolumeUUID) != 36 {
		return fmt.Errorf("workspace storage requires canonical APFS volume UUID")
	}
	for i := range s.VolumeUUID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if s.VolumeUUID[i] != '-' {
				return fmt.Errorf("workspace storage UUID is malformed")
			}
		} else if s.VolumeUUID[i] < '0' || s.VolumeUUID[i] > '9' && s.VolumeUUID[i] < 'a' || s.VolumeUUID[i] > 'f' {
			return fmt.Errorf("workspace storage UUID is malformed")
		}
	}
	relative, err := filepath.Rel(s.MountPoint, s.StateRoot)
	if err != nil || relative == ".." || len(relative) >= 3 && relative[:3] == "../" {
		return fmt.Errorf("workspace state root is outside expected mount")
	}
	return nil
}

// CheckStorage rechecks the trusted external declaration against the mounted
// backing volume before any operation that could create state or a lock.
func CheckStorage(expectation StorageExpectation) error {
	if err := expectation.Validate(); err != nil {
		return err
	}
	return checkStorage(expectation)
}

// WriteEnrolledConfig publishes a fresh private configuration at ConfigPath
// only after the operator-supplied APFS identity and a different output
// filesystem have been checked. It never overwrites an existing config.
func WriteEnrolledConfig(expectation StorageExpectation, data []byte) error {
	if err := expectation.Validate(); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 1<<20 {
		return fmt.Errorf("enrolled configuration is empty or oversized")
	}
	return writeEnrolledConfig(expectation, data)
}
