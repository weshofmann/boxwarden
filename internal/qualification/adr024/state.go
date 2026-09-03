package adr024

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

const (
	maxFixedManifestBytes = 64 << 10
	maxQualifiedToolBytes = 256 << 20
)

type FixedState struct {
	Expected Expected     `json:"-"`
	Manifest FileEvidence `json:"manifest"`
	Tart     FileEvidence `json:"tart"`
	Softnet  FileEvidence `json:"softnet"`
}

// LoadFixedState reads only Boxwarden's fixed installed host state. The Tart
// PID is the sole caller-supplied runtime identity; paths, digests, and the
// trusted operator come from the strictly validated installed manifest.
func LoadFixedState(tartPID int) (FixedState, error) {
	manifestPath := filepath.Join(filepath.Dir(hostx.QualifiedSoftnetPath), "manifest.json")
	manifestEvidence, raw, err := readFixedManifest(manifestPath, filePolicy{
		UID: 0, GID: 0, Mode: 0o444, Links: 1,
		MaxBytes: maxFixedManifestBytes,
	})
	if err != nil {
		return FixedState{}, fmt.Errorf("read fixed installed manifest: %w", err)
	}
	groups, err := os.Getgroups()
	if err != nil {
		return FixedState{}, fmt.Errorf("read observer effective groups: %w", err)
	}
	expected, err := expectedFromManifest(raw, tartPID, os.Getuid(), os.Geteuid(), os.Getgid(), os.Getegid(), groups)
	if err != nil {
		return FixedState{}, fmt.Errorf("derive expected process identity: %w", err)
	}
	manifest, err := hostx.ParseManifest(raw)
	if err != nil {
		return FixedState{}, err
	}
	tart, err := inspectAdmittedArtifact(manifest.Tart.Path, filePolicy{
		UID: -1, GID: -1, Links: 1, SHA256: manifest.Tart.ExecutableSHA256, MaxBytes: maxQualifiedToolBytes,
	})
	if err != nil {
		return FixedState{}, fmt.Errorf("read fixed qualified Tart: %w", err)
	}
	if tart.Mode&0o111 == 0 || tart.Mode&0o6000 != 0 {
		return FixedState{}, errors.New("fixed qualified Tart has unsafe executable mode")
	}
	softnet, err := inspectAdmittedArtifact(manifest.Softnet.Path, filePolicy{
		UID: 0, GID: manifest.Group.ID, Mode: hostx.SoftnetMode, Links: 1,
		SHA256: manifest.Softnet.ExecutableSHA256, MaxBytes: maxQualifiedToolBytes,
	})
	if err != nil {
		return FixedState{}, fmt.Errorf("read fixed qualified Softnet: %w", err)
	}
	return FixedState{Expected: expected, Manifest: manifestEvidence, Tart: tart, Softnet: softnet}, nil
}

func expectedFromManifest(raw []byte, tartPID, realUID, effectiveUID, realGID, effectiveGID int, effectiveGroups []int) (Expected, error) {
	if tartPID <= 0 || tartPID > math.MaxInt32 {
		return Expected{}, errors.New("Tart PID must be a positive signed 32-bit process identifier")
	}
	manifest, err := hostx.ParseManifest(raw)
	if err != nil {
		return Expected{}, err
	}
	if realUID <= 0 || realUID != effectiveUID || realUID != manifest.Operator.UID {
		return Expected{}, errors.New("observer must run unprivileged as the exact manifested operator")
	}
	if realGID < 0 || realGID != effectiveGID {
		return Expected{}, errors.New("observer real and effective primary groups must match")
	}
	if !containsGroup(effectiveGroups, manifest.Group.ID) {
		return Expected{}, errors.New("observer process does not have the manifested operator group effective")
	}
	return Expected{
		TartPID:           tartPID,
		TartExecutable:    manifest.Tart.Path,
		SoftnetExecutable: manifest.Softnet.Path,
		OperatorUID:       manifest.Operator.UID,
		OperatorGID:       realGID,
	}, nil
}

func containsGroup(groups []int, expected int) bool {
	for _, group := range groups {
		if group == expected {
			return true
		}
	}
	return false
}
