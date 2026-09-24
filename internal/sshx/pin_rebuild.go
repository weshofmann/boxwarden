package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const rebuildPinStageDirectory = "ssh-host-pin-transitions"

// TransitionRebuild replaces one immutable host-key pin only for an exact
// journal-authorized old-to-candidate binding. The caller must have validated
// the durable rebuild journal, stopped old generation, and serial-observed new
// key. An exact new pin is idempotent after an ambiguous atomic rename.
func (s *PinStore) TransitionRebuild(ctx context.Context, operationID string, oldBinding, candidateBinding Binding, oldPresent bool, oldDigest string, observed ObservedHostKey) (HostKeyPin, error) {
	if s == nil || !validUUID(operationID) || oldBinding.Validate() != nil || candidateBinding.Validate() != nil ||
		oldBinding.Domain != s.domain.ID || candidateBinding.Domain != s.domain.ID || oldBinding.SessionID != candidateBinding.SessionID ||
		oldBinding.BackendKind != "tart" || candidateBinding.BackendKind != "tart" || oldBinding.BackendObject == candidateBinding.BackendObject {
		return HostKeyPin{}, fmt.Errorf("invalid rebuild pin transition binding")
	}
	if oldPresent {
		decoded, err := hex.DecodeString(oldDigest)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != oldDigest {
			return HostKeyPin{}, fmt.Errorf("invalid old rebuild pin digest")
		}
	} else if oldDigest != "" {
		return HostKeyPin{}, fmt.Errorf("absent old pin cannot have a digest")
	}
	if observed.Algorithm != "ssh-ed25519" {
		return HostKeyPin{}, fmt.Errorf("rebuild host key must be ssh-ed25519")
	}
	public, _, fingerprint, err := parseEd25519PublicKey(observed.PublicKey)
	if err != nil {
		return HostKeyPin{}, fmt.Errorf("invalid serial-observed rebuild host key: %w", err)
	}
	want := HostKeyPin{Version: hostKeyPinVersion, Domain: candidateBinding.Domain, SessionID: candidateBinding.SessionID,
		BackendKind: candidateBinding.BackendKind, BackendObject: candidateBinding.BackendObject,
		Algorithm: observed.Algorithm, PublicKey: public, Fingerprint: fingerprint}
	if err := ctx.Err(); err != nil {
		return HostKeyPin{}, err
	}
	path := filepath.Join(pinDirectory(s.domain.StateRoot), candidateBinding.SessionID+".json")
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		if oldPresent {
			return HostKeyPin{}, fmt.Errorf("witnessed old pin disappeared before rebuild transition")
		}
		return s.Admit(ctx, candidateBinding, observed)
	} else if err != nil {
		return HostKeyPin{}, err
	}
	if current, err := s.Load(ctx, candidateBinding); err == nil {
		if current != want {
			return HostKeyPin{}, fmt.Errorf("candidate host-key pin differs from serial observation")
		}
		return current, nil
	}
	if !oldPresent {
		return HostKeyPin{}, fmt.Errorf("unexpected existing host-key pin without old witness")
	}
	old, err := s.Load(ctx, oldBinding)
	if err != nil {
		return HostKeyPin{}, fmt.Errorf("load exact old rebuild pin: %w", err)
	}
	rawOld, err := json.Marshal(old)
	if err != nil {
		return HostKeyPin{}, err
	}
	digest := sha256.Sum256(rawOld)
	if hex.EncodeToString(digest[:]) != oldDigest {
		return HostKeyPin{}, fmt.Errorf("old host-key pin differs from durable rebuild witness")
	}
	before, err := requirePrivateFile(path)
	if err != nil {
		return HostKeyPin{}, err
	}
	identity := filepath.Join(s.domain.StateRoot, "identity")
	stageDir := filepath.Join(identity, rebuildPinStageDirectory)
	if err := ensurePrivateDirectory(stageDir); err != nil {
		return HostKeyPin{}, err
	}
	stagePath := filepath.Join(stageDir, operationID+".json")
	rawNew, err := json.Marshal(want)
	if err != nil {
		return HostKeyPin{}, err
	}
	if err := writePrivateNew(stagePath, append(rawNew, '\n')); err != nil && !errors.Is(err, os.ErrExist) {
		return HostKeyPin{}, fmt.Errorf("stage rebuild host-key pin: %w", err)
	}
	staged, err := readPrivateFile(stagePath)
	if err != nil || string(staged) != string(append(rawNew, '\n')) {
		return HostKeyPin{}, fmt.Errorf("staged rebuild pin differs from exact candidate: %v", err)
	}
	current, err := requirePrivateFile(path)
	if err != nil || !os.SameFile(before, current) {
		return HostKeyPin{}, fmt.Errorf("old rebuild pin changed before atomic transition: %v", err)
	}
	old, err = s.Load(ctx, oldBinding)
	if err != nil {
		return HostKeyPin{}, err
	}
	rawOld, err = json.Marshal(old)
	if err != nil {
		return HostKeyPin{}, err
	}
	digest = sha256.Sum256(rawOld)
	if hex.EncodeToString(digest[:]) != oldDigest {
		return HostKeyPin{}, fmt.Errorf("old rebuild pin content changed before atomic transition")
	}
	if err := ctx.Err(); err != nil {
		return HostKeyPin{}, err
	}
	root, err := openVerifiedRoot(identity)
	if err != nil {
		return HostKeyPin{}, err
	}
	defer root.Close()
	if err := root.Rename(filepath.Join(rebuildPinStageDirectory, operationID+".json"), filepath.Join("ssh-host-pins", candidateBinding.SessionID+".json")); err != nil {
		return HostKeyPin{}, fmt.Errorf("atomically replace rebuild pin: %w", err)
	}
	if err := syncRebuildPinDirectory(pinDirectory(s.domain.StateRoot)); err != nil {
		return HostKeyPin{}, err
	}
	if err := syncRebuildPinDirectory(stageDir); err != nil {
		return HostKeyPin{}, err
	}
	return want, nil
}

func syncRebuildPinDirectory(path string) error {
	root, err := openVerifiedRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(directory.Sync(), directory.Close())
}
