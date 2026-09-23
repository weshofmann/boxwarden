package backend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const (
	managedTestVolume     = "00112233-4455-4677-8899-aabbccddeeff"
	managedTestGeneration = "10213243-5465-4768-899a-bbccddeeff00"
)

type testDiskACL func(string) (bool, error)

func (f testDiskACL) HasExtendedACL(path string) (bool, error) { return f(path) }

func cleanDiskACL(string) (bool, error) { return false, nil }

func managedDiskFixture(t *testing.T) (string, string, *os.File, *lock.Held) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "volumes"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "volumes", managedTestVolume+".raw")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), root, "volume-work-"+managedTestVolume)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release(); _ = file.Close() })
	return root, path, file, held
}

func testManagedRequest(root string) StartRequest {
	return StartRequest{
		ObjectID:            "boxwarden-work-dev",
		SerialDevice:        "/dev/ttys004",
		GenerationDirectory: filepath.Join(root, "runtime", managedTestGeneration),
	}
}

func TestManagedDiskBindsPrivateRawPathFileAndExactLock(t *testing.T) {
	root, path, file, held := managedDiskFixture(t)
	disk, err := newManagedDisk(root, domain.ID("work"), managedTestVolume, "boxwarden-work-dev", managedTestGeneration, file, held, testDiskACL(cleanDiskACL))
	if err != nil {
		t.Fatal(err)
	}
	request := testManagedRequest(root)
	request.ManagedDisks, err = NewManagedDiskSet(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartRequest(request); err != nil {
		t.Fatal(err)
	}
	claim, err := disk.TakeForStart(request)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Operand() != path {
		t.Fatalf("disk operand = %q, want exact managed path %q", claim.Operand(), path)
	}
	if err := disk.Close(); !errors.Is(err, ErrManagedDiskTransferred) || !held.MatchesScope("volume-work-"+managedTestVolume) {
		t.Fatalf("transferred lock was released early: %v", err)
	}
	if err := claim.Close(); err != nil {
		t.Fatal(err)
	}
	if held.MatchesScope("volume-work-" + managedTestVolume) {
		t.Fatal("exact lock remains held after lifetime close")
	}
}

func TestManagedDiskRefusesWrongLockAndUnsafeFile(t *testing.T) {
	for _, kind := range []string{"wrong lock", "other root lock", "released lock", "hardlink", "symlink", "ACL", "lock ACL", "lock directory ACL", "ACL inspection error", "colon path"} {
		t.Run(kind, func(t *testing.T) {
			root, path, file, held := managedDiskFixture(t)
			inspector := testDiskACL(cleanDiskACL)
			switch kind {
			case "wrong lock":
				held.Release()
				var err error
				held, err = lock.Acquire(context.Background(), root, "volume-personal-"+managedTestVolume)
				if err != nil {
					t.Fatal(err)
				}
				defer held.Release()
			case "other root lock":
				held.Release()
				otherRoot := filepath.Join(t.TempDir(), "other state")
				if err := os.Mkdir(otherRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				var err error
				held, err = lock.Acquire(context.Background(), otherRoot, "volume-work-"+managedTestVolume)
				if err != nil {
					t.Fatal(err)
				}
				defer held.Release()
			case "released lock":
				held.Release()
			case "hardlink":
				if err := os.Link(path, path+".extra"); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+".old", path); err != nil {
					t.Fatal(err)
				}
			case "ACL":
				inspector = func(candidate string) (bool, error) { return candidate == path, nil }
			case "lock ACL":
				lockPath := filepath.Join(root, "locks", "volume-work-"+managedTestVolume+".lock")
				inspector = func(candidate string) (bool, error) { return candidate == lockPath, nil }
			case "lock directory ACL":
				lockDirectory := filepath.Join(root, "locks")
				inspector = func(candidate string) (bool, error) { return candidate == lockDirectory, nil }
			case "ACL inspection error":
				inspector = func(candidate string) (bool, error) {
					if candidate == path {
						return false, errors.New("inspection failed")
					}
					return false, nil
				}
			case "colon path":
				newRoot := root + ":ambiguous"
				if err := os.Rename(root, newRoot); err != nil {
					t.Fatal(err)
				}
				root = newRoot
			}
			if disk, err := newManagedDisk(root, domain.ID("work"), managedTestVolume, "boxwarden-work-dev", managedTestGeneration, file, held, inspector); err == nil {
				disk.Close()
				t.Fatalf("%s was accepted", kind)
			}
		})
	}
}

func TestManagedDiskRejectsWrongLaunchBindingAndReplacement(t *testing.T) {
	root, path, file, held := managedDiskFixture(t)
	disk, err := newManagedDisk(root, domain.ID("work"), managedTestVolume, "boxwarden-work-dev", managedTestGeneration, file, held, testDiskACL(cleanDiskACL))
	if err != nil {
		t.Fatal(err)
	}
	request := testManagedRequest(root)
	request.ManagedDisks, err = NewManagedDiskSet(disk)
	if err != nil {
		t.Fatal(err)
	}
	wrong := request
	wrong.ObjectID = "boxwarden-work-other"
	if err := ValidateStartRequest(wrong); err == nil {
		t.Fatal("different backend object accepted")
	}
	wrong = request
	wrong.GenerationDirectory = filepath.Join(root, "runtime", "11335577-99bb-4cdd-8eff-0123456789ab")
	if err := ValidateStartRequest(wrong); err == nil {
		t.Fatal("different generation accepted")
	}
	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := replacement.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	if err := ValidateStartRequest(request); err == nil {
		t.Fatal("replaced raw path accepted")
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDiskStartRequestRejectsDuplicateAndExcessOperands(t *testing.T) {
	root, _, file, held := managedDiskFixture(t)
	disk, err := newManagedDisk(root, domain.ID("work"), managedTestVolume, "boxwarden-work-dev", managedTestGeneration, file, held, testDiskACL(cleanDiskACL))
	if err != nil {
		t.Fatal(err)
	}
	request := testManagedRequest(root)
	if _, err := NewManagedDiskSet(disk, disk); err == nil {
		t.Fatal("duplicate volume accepted")
	}
	if _, err := NewManagedDiskSet(disk, disk, disk, disk, disk); err == nil {
		t.Fatal("unbounded disk list accepted")
	}
	otherRoot, _, otherFile, otherHeld := managedDiskFixture(t)
	other, err := newManagedDisk(otherRoot, domain.ID("work"), managedTestVolume, "boxwarden-work-dev", managedTestGeneration, otherFile, otherHeld, testDiskACL(cleanDiskACL))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewManagedDiskSet(disk, other); err == nil {
		t.Fatal("managed disks from different state roots accepted together")
	}
	request.ManagedDisks, err = NewManagedDiskSet(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := disk.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	second, err := lock.Acquire(ctx, root, "volume-work-"+managedTestVolume)
	if err != nil {
		t.Fatalf("managed disk close did not release lock: %v", err)
	}
	second.Release()
}
