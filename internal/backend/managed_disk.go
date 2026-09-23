package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const maxManagedDisks = 4

// ErrManagedDiskTransferred means a managed disk's open file and exact use
// lock have moved to a retained Tart handle. Only that handle may release them.
var ErrManagedDiskTransferred = errors.New("managed disk ownership transferred to backend handle")

// ManagedDisk is a one-use, private raw-file lease. It binds a pinned host
// inode and exact volume-use lock to one backend object and generation. This
// is a mechanics contract: common control-plane code must first prove the
// workspace record, reservation, and formatter qualification.
type ManagedDisk struct {
	mu         sync.Mutex
	stateRoot  string
	path       string
	domain     domain.ID
	volumeID   string
	objectID   string
	generation string
	device     uint64
	inode      uint64
	size       int64
	file       *os.File
	held       *lock.Held
	inspector  privateacl.Inspector
	claimed    bool
}

// NewManagedDisk validates and takes ownership of an already admitted file and
// exact volume lock on success. A failed call leaves both with the caller.
func NewManagedDisk(stateRoot string, domainID domain.ID, volumeID, objectID, generation string, file *os.File, held *lock.Held) (*ManagedDisk, error) {
	return newManagedDisk(stateRoot, domainID, volumeID, objectID, generation, file, held, privateacl.OSInspector{})
}

func newManagedDisk(stateRoot string, domainID domain.ID, volumeID, objectID, generation string, file *os.File, held *lock.Held, inspector privateacl.Inspector) (*ManagedDisk, error) {
	if _, err := domain.Parse(string(domainID)); err != nil {
		return nil, err
	}
	if !validManagedUUID(volumeID) || !validManagedUUID(generation) {
		return nil, fmt.Errorf("invalid managed volume or generation UUID")
	}
	if err := ValidateObjectID(objectID); err != nil {
		return nil, err
	}
	if !canonicalAbsolutePath(stateRoot) || strings.Contains(stateRoot, ":") {
		return nil, fmt.Errorf("managed disk root is noncanonical or ambiguous as a Tart operand")
	}
	if file == nil || held == nil || inspector == nil {
		return nil, fmt.Errorf("managed disk requires opened file, exact use lock, and ACL inspector")
	}
	path := filepath.Join(stateRoot, "volumes", volumeID+".raw")
	disk := &ManagedDisk{stateRoot: stateRoot, path: path, domain: domainID, volumeID: volumeID, objectID: objectID, generation: generation, file: file, held: held, inspector: inspector}
	if !held.MatchesExact(stateRoot, disk.lockScope()) {
		return nil, fmt.Errorf("managed disk lacks exact live volume-use lock")
	}
	if err := disk.validatePhysical(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	disk.device, disk.inode, disk.size = uint64(stat.Dev), uint64(stat.Ino), info.Size()
	return disk, nil
}

func validManagedUUID(raw string) bool {
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
		if !((raw[i] >= '0' && raw[i] <= '9') || (raw[i] >= 'a' && raw[i] <= 'f')) {
			return false
		}
	}
	return true
}

func (d *ManagedDisk) lockScope() string {
	return "volume-" + string(d.domain) + "-" + d.volumeID
}

func (d *ManagedDisk) validatePhysical() error {
	for _, path := range []string{d.stateRoot, filepath.Join(d.stateRoot, "volumes"), filepath.Join(d.stateRoot, "locks")} {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !operatorOwned(info) {
			return fmt.Errorf("managed disk directory is not private")
		}
		if err := privateacl.Check(path, info, d.inspector); err != nil {
			return err
		}
	}
	lockPath := filepath.Join(d.stateRoot, "locks", d.lockScope()+".lock")
	lockInfo, err := os.Lstat(lockPath)
	if err != nil {
		return err
	}
	if err := privateManagedFile(lockInfo); err != nil {
		return fmt.Errorf("managed disk lock: %w", err)
	}
	if err := privateacl.Check(lockPath, lockInfo, d.inspector); err != nil {
		return err
	}
	if !d.held.MatchesExact(d.stateRoot, d.lockScope()) {
		return fmt.Errorf("managed disk volume-use lock changed")
	}
	opened, err := d.file.Stat()
	if err != nil {
		return err
	}
	if err := privateManagedFile(opened); err != nil {
		return err
	}
	pathInfo, err := os.Lstat(d.path)
	if err != nil {
		return err
	}
	if err := privateManagedFile(pathInfo); err != nil {
		return err
	}
	if !os.SameFile(opened, pathInfo) {
		return fmt.Errorf("managed disk path no longer names opened file")
	}
	if err := privateacl.Check(d.path, opened, d.inspector); err != nil {
		return err
	}
	if opened.Size() < 4096 || opened.Size() > 1<<43 || opened.Size()%512 != 0 {
		return fmt.Errorf("managed disk size is invalid")
	}
	stat := opened.Sys().(*syscall.Stat_t)
	if d.device != 0 && (d.device != uint64(stat.Dev) || d.inode != uint64(stat.Ino) || d.size != opened.Size()) {
		return fmt.Errorf("managed disk identity or size changed")
	}
	return nil
}

func privateManagedFile(info os.FileInfo) error {
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !operatorOwned(info) {
		return fmt.Errorf("managed disk is not a private regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Nlink != 1 || stat.Dev == 0 || stat.Ino == 0 {
		return fmt.Errorf("managed disk has unsafe link or identity")
	}
	return nil
}

func operatorOwned(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}

// CheckForStart does not consume the lease. It verifies the exact target and
// rechecks the file and live lock immediately before Tart admission.
func (d *ManagedDisk) CheckForStart(request StartRequest) error {
	if d == nil {
		return fmt.Errorf("nil managed disk")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.checkLocked(request)
}

func (d *ManagedDisk) checkLocked(request StartRequest) error {
	if d.claimed || d.file == nil || d.held == nil {
		return fmt.Errorf("managed disk lease is unavailable")
	}
	if request.ObjectID != d.objectID || filepath.Base(request.GenerationDirectory) != d.generation {
		return fmt.Errorf("managed disk target does not match exact backend generation")
	}
	if !d.held.MatchesExact(d.stateRoot, d.lockScope()) {
		return fmt.Errorf("managed disk volume-use lock is not live")
	}
	return d.validatePhysical()
}

// TakeForStart transfers the open file and lock to the backend lifetime.
func (d *ManagedDisk) TakeForStart(request StartRequest) (*ManagedDiskLifetime, error) {
	if d == nil {
		return nil, fmt.Errorf("nil managed disk")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.checkLocked(request); err != nil {
		return nil, err
	}
	lifetime := &ManagedDiskLifetime{path: d.path, file: d.file, held: d.held}
	d.file, d.held, d.claimed = nil, nil, true
	return lifetime, nil
}

// Close releases an unclaimed lease. After transfer, only the retained backend
// handle can close its lifetime, so accidental caller cleanup cannot unlock it.
func (d *ManagedDisk) Close() error {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.claimed {
		return ErrManagedDiskTransferred
	}
	if d.file == nil && d.held == nil {
		return nil
	}
	err := errors.Join(d.file.Close(), d.held.Release())
	d.file, d.held = nil, nil
	return err
}

// ManagedDiskLifetime retains the exact file and lock across Tart execution.
type ManagedDiskLifetime struct {
	path string
	file *os.File
	held *lock.Held
	once sync.Once
	err  error
}

func (l *ManagedDiskLifetime) Operand() string {
	if l == nil {
		return ""
	}
	return l.path
}

// Close is for a failed spawn or a retained handle after exact process reap.
func (l *ManagedDiskLifetime) Close() error {
	if l == nil {
		return nil
	}
	l.once.Do(func() { l.err = errors.Join(l.file.Close(), l.held.Release()) })
	return l.err
}

// ManagedDiskSet bounds disk count and keeps one Tart invocation within one
// state root and security domain, without duplicate writable attachment.
// Its contents are unexported.
type ManagedDiskSet struct {
	disks []*ManagedDisk
}

func NewManagedDiskSet(disks ...*ManagedDisk) (*ManagedDiskSet, error) {
	if len(disks) == 0 || len(disks) > maxManagedDisks || disks[0] == nil {
		return nil, fmt.Errorf("managed disk count must be 1..%d", maxManagedDisks)
	}
	seen := map[string]bool{}
	root, domainID := disks[0].stateRoot, disks[0].domain
	for _, disk := range disks {
		if disk == nil || seen[disk.path] || disk.stateRoot != root || disk.domain != domainID {
			return nil, fmt.Errorf("nil, duplicate, or cross-domain/root managed disk")
		}
		seen[disk.path] = true
	}
	return &ManagedDiskSet{disks: append([]*ManagedDisk(nil), disks...)}, nil
}

func (set *ManagedDiskSet) ValidateForStart(request StartRequest) error {
	if set == nil || len(set.disks) == 0 || len(set.disks) > maxManagedDisks || set.disks[0] == nil {
		return fmt.Errorf("invalid managed disk set")
	}
	seen := map[string]bool{}
	root, domainID := set.disks[0].stateRoot, set.disks[0].domain
	for _, disk := range set.disks {
		if disk == nil || seen[disk.path] || disk.stateRoot != root || disk.domain != domainID {
			return fmt.Errorf("nil, duplicate, or cross-domain/root managed disk")
		}
		seen[disk.path] = true
		if err := disk.CheckForStart(request); err != nil {
			return err
		}
	}
	return nil
}

// TakeForStart transfers all leases. On a partial failure it closes every
// already transferred lifetime, leaving no ambiguous unlocked live VM.
func (set *ManagedDiskSet) TakeForStart(request StartRequest) ([]*ManagedDiskLifetime, error) {
	if err := set.ValidateForStart(request); err != nil {
		return nil, err
	}
	lifetimes := make([]*ManagedDiskLifetime, 0, len(set.disks))
	for _, disk := range set.disks {
		lifetime, err := disk.TakeForStart(request)
		if err != nil {
			for _, owned := range lifetimes {
				_ = owned.Close()
			}
			return nil, err
		}
		lifetimes = append(lifetimes, lifetime)
	}
	return lifetimes, nil
}
