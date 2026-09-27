// Package workspaceformat creates and qualifies new, private raw ext4 files.
// It never mounts or formats a disk on the macOS host. The injected Formatter
// must run in a fresh trusted Linux VM and return only after that VM is stopped
// and reaped. This package's superblock read checks magic and UUID; it is not
// a filesystem health or security inspection.
package workspaceformat

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

var aclInspector privateacl.Inspector = privateacl.OSInspector{}

func checkPrivateACL(path string, expected os.FileInfo) error {
	return privateacl.Check(path, expected, aclInspector)
}

const journalVersion = 1
const maxJournalBytes = 4096

// Request deliberately has no caller-selected disk path or host device.
type Request struct {
	Domain         domain.ID
	VolumeID       string
	FilesystemUUID string
	SizeBytes      int64
}

type DiskIdentity struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

// FormatRequest names only the newly created managed raw file. The formatter
// must attach that file alone to an isolated Linux guest, use whole-device
// mkfs.ext4 -U with the requested UUID, run an independent filesystem check,
// and stop/wait/reap the guest before returning.
type FormatRequest struct {
	DiskPath       string
	Domain         domain.ID
	VolumeID       string
	FilesystemUUID string
	SizeBytes      int64
	Identity       DiskIdentity
	// Marker identifies the fresh disk inside the formatter guest before mkfs.
	// It is random per attempt and has no meaning after formatting.
	Marker string
}

// FormatEvidence is a trusted formatter result. Host superblock inspection
// separately checks the ext4 magic and UUID, but cannot establish that e2fsck
// ran, that the filesystem is healthy, or that guest content is safe.
type FormatEvidence struct {
	ObservedUUID    string `json:"observed_uuid"`
	WholeDevice     bool   `json:"whole_device"`
	FilesystemClean bool   `json:"filesystem_clean"`
}

type Formatter interface {
	FormatAndVerify(context.Context, FormatRequest) (FormatEvidence, error)
}

type JournalState string

const (
	StateReserved   JournalState = "reserved"
	StateFormatting JournalState = "formatting"
	StateVerified   JournalState = "verified"
	StateFailed     JournalState = "failed"
)

// Journal is append-only in meaning: any existing journal prevents Create
// from reformatting its volume ID, including interrupted and failed phases.
type Journal struct {
	Version        int             `json:"version"`
	Domain         domain.ID       `json:"domain"`
	VolumeID       string          `json:"volume_id"`
	FilesystemUUID string          `json:"filesystem_uuid"`
	SizeBytes      int64           `json:"size_bytes"`
	State          JournalState    `json:"state"`
	Identity       *DiskIdentity   `json:"identity,omitempty"`
	Evidence       *FormatEvidence `json:"evidence,omitempty"`
}

type Qualification struct {
	Domain         domain.ID
	VolumeID       string
	FilesystemUUID string
	SizeBytes      int64
	Identity       DiskIdentity
}

// Create reserves one volume ID under the domain storage lock, exclusively
// creates its sparse file, calls the trusted Linux formatter, and verifies the
// exact file and ext4 signature before marking the journal verified. Failure
// leaves an unqualified journal and file for investigation; the same ID is
// never retried or silently reformatted.
func Create(ctx context.Context, stateRoot string, request Request, formatter Formatter) (result Qualification, err error) {
	if err := validateRequest(request); err != nil {
		return Qualification{}, err
	}
	if formatter == nil {
		return Qualification{}, fmt.Errorf("nil workspace formatter")
	}
	if err := validateStateRootPath(stateRoot); err != nil {
		return Qualification{}, err
	}
	held, err := lock.Acquire(ctx, stateRoot, "storage-"+string(request.Domain))
	if err != nil {
		return Qualification{}, err
	}
	defer held.Release()
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return Qualification{}, err
	}
	defer root.Close()
	volumes, err := openVolumes(root, true)
	if err != nil {
		return Qualification{}, err
	}
	defer volumes.Close()
	dir, err := volumes.Open(".")
	if err != nil {
		return Qualification{}, err
	}
	defer dir.Close()
	if err := checkHeadroom(dir); err != nil {
		return Qualification{}, err
	}
	journal := Journal{Version: journalVersion, Domain: request.Domain, VolumeID: request.VolumeID, FilesystemUUID: request.FilesystemUUID, SizeBytes: request.SizeBytes, State: StateReserved}
	if err := writeInitialJournal(volumes, journal); err != nil {
		return Qualification{}, fmt.Errorf("reserve workspace volume: %w", err)
	}
	defer func() {
		if err != nil {
			journal.State = StateFailed
			if saveErr := replaceJournal(volumes, journal); saveErr != nil {
				err = errors.Join(err, fmt.Errorf("record failed format: %w", saveErr))
			}
		}
	}()

	name := rawName(request.VolumeID)
	file, createErr := volumes.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if createErr != nil {
		return Qualification{}, fmt.Errorf("create exclusive raw file: %w", createErr)
	}
	defer file.Close()
	if err := file.Truncate(request.SizeBytes); err != nil {
		return Qualification{}, fmt.Errorf("size raw file: %w", err)
	}
	var marker [32]byte
	if _, err := rand.Read(marker[:]); err != nil {
		return Qualification{}, fmt.Errorf("generate raw disk marker: %w", err)
	}
	if _, err := file.WriteAt(marker[:], 0); err != nil {
		return Qualification{}, fmt.Errorf("write raw disk marker: %w", err)
	}
	if err := file.Sync(); err != nil {
		return Qualification{}, fmt.Errorf("sync raw file: %w", err)
	}
	if err := syncDirectory(volumes); err != nil {
		return Qualification{}, fmt.Errorf("sync raw-file directory: %w", err)
	}
	identity, err := exactFile(volumes, name, file, request.SizeBytes)
	if err != nil {
		return Qualification{}, err
	}
	journal.Identity = &identity
	journal.State = StateFormatting
	if err := replaceJournal(volumes, journal); err != nil {
		return Qualification{}, fmt.Errorf("persist formatting intent: %w", err)
	}
	formatRequest := FormatRequest{DiskPath: filepath.Join(stateRoot, "volumes", name), Domain: request.Domain, VolumeID: request.VolumeID, FilesystemUUID: request.FilesystemUUID, SizeBytes: request.SizeBytes, Identity: identity, Marker: hex.EncodeToString(marker[:])}
	formatContext, cancelFormat := context.WithCancel(ctx)
	monitorDone := make(chan error, 1)
	go monitorHeadroom(formatContext, dir, cancelFormat, monitorDone)
	evidence, formatErr := formatter.FormatAndVerify(formatContext, formatRequest)
	cancelFormat()
	spaceErr := <-monitorDone
	if spaceErr != nil {
		return Qualification{}, fmt.Errorf("formatting exceeded host disk reserve: %w", spaceErr)
	}
	if err := ctx.Err(); err != nil {
		return Qualification{}, fmt.Errorf("workspace formatting canceled: %w", err)
	}
	if err := checkHeadroom(dir); err != nil {
		return Qualification{}, err
	}
	err = formatErr
	if err != nil {
		return Qualification{}, fmt.Errorf("Linux formatter: %w", err)
	}
	if evidence.ObservedUUID != request.FilesystemUUID || !evidence.WholeDevice || !evidence.FilesystemClean {
		return Qualification{}, fmt.Errorf("Linux formatter did not verify exact healthy whole-device ext4 filesystem")
	}
	if err := file.Sync(); err != nil {
		return Qualification{}, fmt.Errorf("sync formatted raw file: %w", err)
	}
	postIdentity, err := exactFile(volumes, name, file, request.SizeBytes)
	if err != nil {
		return Qualification{}, err
	}
	if postIdentity != identity {
		return Qualification{}, fmt.Errorf("raw disk identity changed during formatting")
	}
	if err := verifyExt4Header(file, request.FilesystemUUID); err != nil {
		return Qualification{}, err
	}
	journal.Evidence = &evidence
	journal.State = StateVerified
	if err := replaceJournal(volumes, journal); err != nil {
		return Qualification{}, fmt.Errorf("persist verified format: %w", err)
	}
	return qualification(journal), nil
}

// Admit rechecks the verified journal, private path, exact inode/device,
// length, and ext4 magic/UUID. Callers must retain the returned file and the
// volume-use lock through Tart launch and the backend stop/wait/reap path.
func Admit(stateRoot string, request Request) (*os.File, Qualification, error) {
	if err := validateRequest(request); err != nil {
		return nil, Qualification{}, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, Qualification{}, err
	}
	defer root.Close()
	volumes, err := openVolumes(root, false)
	if err != nil {
		return nil, Qualification{}, err
	}
	defer volumes.Close()
	journal, err := readJournal(volumes, request)
	if err != nil {
		return nil, Qualification{}, err
	}
	if journal.State != StateVerified || journal.Identity == nil || journal.Evidence == nil || journal.Evidence.ObservedUUID != request.FilesystemUUID || !journal.Evidence.WholeDevice || !journal.Evidence.FilesystemClean {
		return nil, Qualification{}, fmt.Errorf("workspace format is not verified")
	}
	name := rawName(request.VolumeID)
	info, err := volumes.Lstat(name)
	if err != nil {
		return nil, Qualification{}, err
	}
	if err := privateRegular(info); err != nil {
		return nil, Qualification{}, err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), name), info); err != nil {
		return nil, Qualification{}, err
	}
	file, err := volumes.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, Qualification{}, err
	}
	identity, err := exactFile(volumes, name, file, request.SizeBytes)
	if err == nil && identity != *journal.Identity {
		err = fmt.Errorf("raw disk identity does not match qualification")
	}
	if err == nil {
		err = verifyExt4Header(file, request.FilesystemUUID)
	}
	if err != nil {
		file.Close()
		return nil, Qualification{}, err
	}
	return file, qualification(journal), nil
}

// ReadJournal is a read-only recovery diagnostic. Reserved, formatting, and
// failed journals are evidence of an incomplete operation, never resume tokens.
func ReadJournal(stateRoot string, request Request) (Journal, error) {
	if err := validateRequest(request); err != nil {
		return Journal{}, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return Journal{}, err
	}
	defer root.Close()
	volumes, err := openVolumes(root, false)
	if err != nil {
		return Journal{}, err
	}
	defer volumes.Close()
	return readJournal(volumes, request)
}

func qualification(journal Journal) Qualification {
	return Qualification{Domain: journal.Domain, VolumeID: journal.VolumeID, FilesystemUUID: journal.FilesystemUUID, SizeBytes: journal.SizeBytes, Identity: *journal.Identity}
}

func validateRequest(request Request) error {
	if _, err := domain.Parse(string(request.Domain)); err != nil {
		return err
	}
	if !validUUID(request.VolumeID) || !validUUID(request.FilesystemUUID) || request.SizeBytes < 4096 || request.SizeBytes > 1<<43 || request.SizeBytes%512 != 0 {
		return fmt.Errorf("invalid workspace formatting request")
	}
	return nil
}

func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i := range raw {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if raw[i] != '-' {
				return false
			}
			continue
		}
		if raw[i] < '0' || raw[i] > '9' && raw[i] < 'a' || raw[i] > 'f' {
			return false
		}
	}
	return true
}

func validateStateRootPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("state root must be a clean absolute path")
	}
	return nil
}

func rawName(id string) string     { return id + ".raw" }
func journalName(id string) string { return id + ".format.json" }

func openStateRoot(path string) (*os.Root, error) {
	if err := validateStateRootPath(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(info); err != nil {
		return nil, err
	}
	if err := checkPrivateACL(path, info); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("state root changed while opening: %v", err)
	}
	if err := privateDirectory(opened); err != nil {
		root.Close()
		return nil, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		root.Close()
		return nil, err
	}
	return root, nil
}

func openVolumes(root *os.Root, create bool) (*os.Root, error) {
	info, err := root.Lstat("volumes")
	if errors.Is(err, os.ErrNotExist) && create {
		if err := root.Mkdir("volumes", 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := syncDirectory(root); err != nil {
			return nil, err
		}
		info, err = root.Lstat("volumes")
	}
	if err != nil {
		return nil, err
	}
	if err := privateDirectory(info); err != nil {
		return nil, err
	}
	path := filepath.Join(root.Name(), "volumes")
	if err := checkPrivateACL(path, info); err != nil {
		return nil, err
	}
	volumes, err := root.OpenRoot("volumes")
	if err != nil {
		return nil, err
	}
	opened, err := volumes.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		volumes.Close()
		return nil, fmt.Errorf("volumes directory changed while opening: %v", err)
	}
	if err := privateDirectory(opened); err != nil {
		volumes.Close()
		return nil, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		volumes.Close()
		return nil, err
	}
	return volumes, nil
}

func privateDirectory(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("expected private directory mode 0700")
	}
	return ownedByOperator(info)
}

func privateRegular(info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf("expected private regular file mode 0600")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 {
		return fmt.Errorf("private file must have exactly one link")
	}
	return ownedByOperator(info)
}

func ownedByOperator(info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("file owner differs from operator")
	}
	return nil
}

func diskIdentity(info os.FileInfo) (DiskIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev == 0 || stat.Ino == 0 {
		return DiskIdentity{}, fmt.Errorf("raw disk identity unavailable")
	}
	return DiskIdentity{Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}

func exactFile(volumes *os.Root, name string, file *os.File, size int64) (DiskIdentity, error) {
	opened, err := file.Stat()
	if err != nil {
		return DiskIdentity{}, err
	}
	if err := privateRegular(opened); err != nil {
		return DiskIdentity{}, err
	}
	if opened.Size() != size {
		return DiskIdentity{}, fmt.Errorf("raw disk size changed")
	}
	entry, err := volumes.Lstat(name)
	if err != nil {
		return DiskIdentity{}, err
	}
	if err := privateRegular(entry); err != nil {
		return DiskIdentity{}, err
	}
	if !os.SameFile(opened, entry) {
		return DiskIdentity{}, fmt.Errorf("raw disk path no longer names pinned file")
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), name), opened); err != nil {
		return DiskIdentity{}, err
	}
	return diskIdentity(opened)
}

func verifyExt4Header(file *os.File, expectedUUID string) error {
	var magic [2]byte
	if _, err := file.ReadAt(magic[:], 1024+0x38); err != nil {
		return err
	}
	if magic != [2]byte{0x53, 0xef} {
		return fmt.Errorf("raw disk lacks whole-device ext4 superblock magic")
	}
	var observed [16]byte
	if _, err := file.ReadAt(observed[:], 1024+0x68); err != nil {
		return err
	}
	expected, err := hex.DecodeString(strings.ReplaceAll(expectedUUID, "-", ""))
	if err != nil || !bytes.Equal(observed[:], expected) {
		return fmt.Errorf("raw disk ext4 UUID does not match qualification")
	}
	return nil
}

func syncDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func checkHeadroom(directory *os.File) error {
	var stat syscall.Statfs_t
	if err := syscall.Fstatfs(int(directory.Fd()), &stat); err != nil {
		return err
	}
	if stat.Bsize <= 0 {
		return fmt.Errorf("invalid host filesystem block size")
	}
	blockSize := uint64(stat.Bsize)
	blocks, available := uint64(stat.Blocks), uint64(stat.Bavail)
	if blocks > ^uint64(0)/blockSize || available > ^uint64(0)/blockSize {
		return fmt.Errorf("host filesystem capacity overflow")
	}
	return checkHeadroomValues(blocks*blockSize, available*blockSize)
}

func checkHeadroomValues(capacity, free uint64) error {
	const twentyGiB = uint64(20) << 30
	floor := capacity / 10
	if floor < twentyGiB {
		floor = twentyGiB
	}
	if free <= floor {
		return fmt.Errorf("host filesystem free space %d is at or below reserve %d", free, floor)
	}
	return nil
}

func monitorHeadroom(ctx context.Context, directory *os.File, cancel context.CancelFunc, done chan<- error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			done <- nil
			return
		case <-ticker.C:
			if err := checkHeadroom(directory); err != nil {
				cancel()
				done <- err
				return
			}
		}
	}
}

func writeInitialJournal(volumes *os.Root, journal Journal) error {
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	file, err := volumes.OpenFile(journalName(journal.VolumeID), os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if err := privateRegular(info); err != nil {
		file.Close()
		return err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), journalName(journal.VolumeID)), info); err != nil {
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
	return syncDirectory(volumes)
}

func replaceJournal(volumes *os.Root, journal Journal) error {
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name := journalName(journal.VolumeID)
	existingInfo, err := volumes.Lstat(name)
	if err != nil {
		return err
	}
	if err := privateRegular(existingInfo); err != nil {
		return err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), name), existingInfo); err != nil {
		return err
	}
	temp := name + ".tmp-" + hex.EncodeToString(nonce[:])
	file, err := volumes.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	defer volumes.Remove(temp)
	tempInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if err := privateRegular(tempInfo); err != nil {
		file.Close()
		return err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), temp), tempInfo); err != nil {
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
	if err := volumes.Rename(temp, name); err != nil {
		return err
	}
	publishedInfo, err := volumes.Lstat(name)
	if err != nil || !os.SameFile(tempInfo, publishedInfo) {
		return fmt.Errorf("format journal changed during publication: %v", err)
	}
	if err := privateRegular(publishedInfo); err != nil {
		return err
	}
	if err := checkPrivateACL(filepath.Join(volumes.Name(), name), publishedInfo); err != nil {
		return err
	}
	return syncDirectory(volumes)
}

func readJournal(volumes *os.Root, request Request) (Journal, error) {
	name := journalName(request.VolumeID)
	info, err := volumes.Lstat(name)
	if err != nil {
		return Journal{}, err
	}
	if err := privateRegular(info); err != nil {
		return Journal{}, err
	}
	path := filepath.Join(volumes.Name(), name)
	if err := checkPrivateACL(path, info); err != nil {
		return Journal{}, err
	}
	file, err := volumes.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Journal{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return Journal{}, fmt.Errorf("format journal changed while opening: %v", err)
	}
	if err := privateRegular(opened); err != nil {
		return Journal{}, err
	}
	if err := checkPrivateACL(path, opened); err != nil {
		return Journal{}, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxJournalBytes+1))
	if err != nil || len(raw) > maxJournalBytes {
		return Journal{}, fmt.Errorf("format journal unreadable or oversized: %v", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := rejectDuplicateJSON(raw); err != nil {
		return Journal{}, err
	}
	var journal Journal
	if err := decoder.Decode(&journal); err != nil {
		return Journal{}, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Journal{}, fmt.Errorf("trailing format journal content")
	}
	if journal.Version != journalVersion || journal.Domain != request.Domain || journal.VolumeID != request.VolumeID || journal.FilesystemUUID != request.FilesystemUUID || journal.SizeBytes != request.SizeBytes {
		return Journal{}, fmt.Errorf("format journal does not match exact request")
	}
	switch journal.State {
	case StateReserved:
		if journal.Identity != nil || journal.Evidence != nil {
			return Journal{}, fmt.Errorf("reserved format journal claims completed work")
		}
	case StateFormatting, StateFailed:
		if journal.Evidence != nil {
			return Journal{}, fmt.Errorf("incomplete format journal claims verification")
		}
	case StateVerified:
		if journal.Identity == nil || journal.Identity.Device == 0 || journal.Identity.Inode == 0 || journal.Evidence == nil {
			return Journal{}, fmt.Errorf("verified format journal lacks proof")
		}
	default:
		return Journal{}, fmt.Errorf("invalid format journal state")
	}
	return journal, nil
}

// encoding/json accepts duplicate keys by default. A journal must have one
// unambiguous value for every field, including fields in nested proof objects.
func rejectDuplicateJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return fmt.Errorf("trailing JSON value")
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = true
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if end != matchingDelim(delim) {
		return fmt.Errorf("mismatched JSON delimiter")
	}
	return nil
}

func matchingDelim(start json.Delim) json.Delim {
	if start == '{' {
		return '}'
	}
	return ']'
}
