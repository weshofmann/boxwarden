package workspaceformat

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

const legacyBindingVersion = 1
const maxLegacyBindingBytes = 4096

// legacyBinding is a write-once reconciliation proof for one exact v1
// formatter receipt. It does not change the historical record or receipt.
type legacyBinding struct {
	Version        int                   `json:"version"`
	Domain         domain.ID             `json:"domain"`
	VolumeID       string                `json:"volume_id"`
	FilesystemUUID string                `json:"filesystem_uuid"`
	SizeBytes      int64                 `json:"size_bytes"`
	Original       DiskIdentity          `json:"original_identity"`
	ReceiptSHA256  string                `json:"receipt_sha256"`
	HostIdentity   hostidentity.Identity `json:"host_identity"`
}

func legacyBindingName(volumeID string) string { return volumeID + ".binding.json" }

// BindLegacy re-proves an exact verified v1 disk using the independently
// enrolled APFS volume UUID and publishes a private, write-once binding.
// The caller must hold the volume-use and storage locks and prove the exact
// backend stopped before calling this function.
func BindLegacy(stateRoot string, request Request, storage hostidentity.StorageExpectation) error {
	return bindLegacy(stateRoot, request, storage, hostidentity.Observe, hostidentity.CheckStorage)
}

func bindLegacy(stateRoot string, request Request, storage hostidentity.StorageExpectation,
	observe func(*os.File) (hostidentity.Identity, error), check func(hostidentity.StorageExpectation) error) error {
	return bindLegacyWithHook(stateRoot, request, storage, observe, check, nil)
}

func bindLegacyWithHook(stateRoot string, request Request, storage hostidentity.StorageExpectation,
	observe func(*os.File) (hostidentity.Identity, error), check func(hostidentity.StorageExpectation) error,
	hook func(bindingPublicationStage) error) error {
	if err := validateRequest(request); err != nil {
		return err
	}
	if err := storage.Validate(); err != nil {
		return err
	}
	if storage.StateRoot != stateRoot || observe == nil || check == nil {
		return fmt.Errorf("legacy binding lacks exact storage or identity checker")
	}
	if err := check(storage); err != nil {
		return fmt.Errorf("admit enrolled storage before legacy binding: %w", err)
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	volumes, err := openVolumes(root, false)
	if err != nil {
		return err
	}
	defer volumes.Close()
	journal, rawReceipt, err := readJournalWithRaw(volumes, request)
	if err != nil {
		return err
	}
	if journal.Version != 1 || journal.State != StateVerified || journal.Identity == nil || journal.Evidence == nil ||
		journal.Evidence.ObservedUUID != request.FilesystemUUID || !journal.Evidence.WholeDevice || !journal.Evidence.FilesystemClean {
		return fmt.Errorf("legacy receipt is not verified")
	}
	name := rawName(request.VolumeID)
	file, err := volumes.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	identity, err := exactFile(volumes, name, file, request.SizeBytes)
	if err != nil {
		return err
	}
	if identity.Inode != journal.Identity.Inode {
		return fmt.Errorf("legacy raw file ID differs from verified receipt")
	}
	if err := verifyExt4Header(file, request.FilesystemUUID); err != nil {
		return err
	}
	host, err := observe(file)
	if err != nil {
		return err
	}
	if host.VolumeUUID != storage.VolumeUUID || host.FileID != identity.Inode {
		return fmt.Errorf("legacy raw file differs from expected persistent APFS identity")
	}
	digest := sha256.Sum256(rawReceipt)
	want := legacyBinding{Version: legacyBindingVersion, Domain: request.Domain, VolumeID: request.VolumeID,
		FilesystemUUID: request.FilesystemUUID, SizeBytes: request.SizeBytes, Original: *journal.Identity,
		ReceiptSHA256: hex.EncodeToString(digest[:]), HostIdentity: host}
	existing, err := readLegacyBinding(volumes, request, journal, rawReceipt)
	if err != nil {
		return err
	}
	if existing != nil {
		if *existing != want {
			return fmt.Errorf("existing legacy binding conflicts with pinned proof")
		}
		if err := check(storage); err != nil {
			return fmt.Errorf("recheck enrolled storage before binding settlement: %w", err)
		}
		return syncDirectory(volumes)
	}
	if err := check(storage); err != nil {
		return fmt.Errorf("recheck enrolled storage before legacy publication: %w", err)
	}
	if err := writeLegacyBindingWithHook(volumes, want, hook); err != nil {
		return err
	}
	published, err := readLegacyBinding(volumes, request, journal, rawReceipt)
	if err != nil || published == nil || *published != want {
		return fmt.Errorf("published legacy binding differs from exact proof: %v", err)
	}
	if err := check(storage); err != nil {
		return fmt.Errorf("recheck enrolled storage after binding publication: %w", err)
	}
	return syncDirectory(volumes)
}

func readLegacyBinding(volumes *os.Root, request Request, journal Journal, rawReceipt []byte) (*legacyBinding, error) {
	name := legacyBindingName(request.VolumeID)
	info, err := volumes.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := privateRegular(info); err != nil {
		return nil, err
	}
	path := filepath.Join(volumes.Name(), name)
	if err := checkPrivateACL(path, info); err != nil {
		return nil, err
	}
	file, err := volumes.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("legacy binding changed while opening: %v", err)
	}
	if err := privateRegular(opened); err != nil {
		return nil, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxLegacyBindingBytes+1))
	if err != nil || len(raw) > maxLegacyBindingBytes {
		return nil, fmt.Errorf("legacy binding unreadable or oversized: %v", err)
	}
	if err := rejectDuplicateJSON(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var binding legacyBinding
	if err := decoder.Decode(&binding); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing legacy binding content")
	}
	digest := sha256.Sum256(rawReceipt)
	if journal.Version != 1 || journal.State != StateVerified || journal.Identity == nil ||
		binding.Version != legacyBindingVersion || binding.Domain != request.Domain || binding.VolumeID != request.VolumeID ||
		binding.FilesystemUUID != request.FilesystemUUID || binding.SizeBytes != request.SizeBytes || binding.Original != *journal.Identity ||
		binding.ReceiptSHA256 != hex.EncodeToString(digest[:]) || !validUUID(binding.HostIdentity.VolumeUUID) ||
		binding.HostIdentity.FileID != journal.Identity.Inode {
		return nil, fmt.Errorf("legacy binding does not match exact verified receipt")
	}
	return &binding, nil
}

type bindingPublicationStage string

const (
	bindingBeforeLink  bindingPublicationStage = "before_link"
	bindingAfterLink   bindingPublicationStage = "after_link"
	bindingAfterRemove bindingPublicationStage = "after_remove"
	bindingBeforeSync  bindingPublicationStage = "before_sync"
	bindingAfterSync   bindingPublicationStage = "after_sync"
)

func writeLegacyBindingWithHook(volumes *os.Root, binding legacyBinding, hook func(bindingPublicationStage) error) error {
	raw, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := legacyBindingName(binding.VolumeID)
	temp := name + ".tmp-" + hex.EncodeToString(nonce[:])
	file, err := volumes.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer volumes.Remove(temp)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if err := privateRegular(info); err != nil {
		file.Close()
		return err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), temp), info); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(append(raw, '\n')); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if hook != nil {
		if err := hook(bindingBeforeLink); err != nil {
			return err
		}
	}
	if err := volumes.Link(temp, name); err != nil {
		return err
	}
	if hook != nil {
		if err := hook(bindingAfterLink); err != nil {
			return err
		}
	}
	if err := volumes.Remove(temp); err != nil {
		return err
	}
	if hook != nil {
		if err := hook(bindingAfterRemove); err != nil {
			return err
		}
		if err := hook(bindingBeforeSync); err != nil {
			return err
		}
	}
	if err := syncDirectory(volumes); err != nil {
		return err
	}
	if hook != nil {
		return hook(bindingAfterSync)
	}
	return nil
}
