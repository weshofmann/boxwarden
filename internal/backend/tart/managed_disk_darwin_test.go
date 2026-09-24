//go:build darwin

package tart

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

const (
	testDiskVolume     = "00112233-4455-4677-8899-aabbccddeeff"
	testDiskGeneration = "10213243-5465-4768-899a-bbccddeeff00"
)

func tartManagedDiskFixture(t *testing.T) (backend.StartRequest, string, *lock.Held) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state with spaces")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "volumes"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "volumes", testDiskVolume+".raw")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	held, err := lock.Acquire(context.Background(), root, "volume-work-"+testDiskVolume)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Release(); _ = file.Close() })
	disk, err := backend.NewManagedDisk(root, domain.ID("work"), testDiskVolume, "boxwarden-work-dev", testDiskGeneration, file, held)
	if err != nil {
		t.Fatal(err)
	}
	set, err := backend.NewManagedDiskSet(disk)
	if err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtime, 0o700); err != nil {
		t.Fatal(err)
	}
	generation := filepath.Join(runtime, testDiskGeneration)
	if err := os.Mkdir(generation, 0o700); err != nil {
		t.Fatal(err)
	}
	return backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation, ManagedDisks: set}, path, held
}

func TestLauncherPassesExactManagedRawPathAndRetainsLeaseUntilReap(t *testing.T) {
	request, path, held := tartManagedDiskFixture(t)
	process := &recordingProcessStarter{handle: &processHandleFake{}}
	handle, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", "/dev/ttys004", "--disk", path, "boxwarden-work-dev"}
	if !sameLifecycleStrings(process.spec.args, want) {
		t.Fatalf("child argv = %#v, want %#v", process.spec.args, want)
	}
	if !held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("volume lock released before exact reap")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := handle.Wait(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Wait = %v", err)
	}
	if !held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("canceled wait released volume lock")
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("volume lock retained after exact reap")
	}
}

func TestLauncherReleasesManagedDiskAfterFailedSpawn(t *testing.T) {
	request, _, held := tartManagedDiskFixture(t)
	process := &recordingProcessStarter{}
	if handle, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), request); err == nil || handle != nil {
		t.Fatalf("failed spawn = (%v, %v)", handle, err)
	}
	if held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("failed spawn retained volume lock")
	}
}

type guestRequestProcessHandle struct {
	processHandleFake
	requests int
	live     bool
}

func (h *guestRequestProcessHandle) RequestStop(context.Context) error {
	h.requests++
	return nil
}

func (h *guestRequestProcessHandle) RetainedChildLive() bool { return h.live }

func TestManagedDiskHandleForwardsRetainedChildLiveness(t *testing.T) {
	request, _, _ := tartManagedDiskFixture(t)
	child := &guestRequestProcessHandle{live: true}
	handle, err := newLauncher(validLaunchConfig(), &recordingProcessStarter{handle: child}).Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	liveness, ok := handle.(interface{ RetainedChildLive() bool })
	if !ok || !liveness.RetainedChildLive() {
		t.Fatal("managed disk and scratch wrappers hid retained child liveness")
	}
	child.live = false
	if liveness.RetainedChildLive() {
		t.Fatal("wrapper reported reaped child live")
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManagedDiskHandleForwardsGuestShutdownWithoutReleasingLease(t *testing.T) {
	request, _, held := tartManagedDiskFixture(t)
	processHandle := &guestRequestProcessHandle{}
	handle, err := newLauncher(validLaunchConfig(), &recordingProcessStarter{handle: processHandle}).Start(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	requester, ok := handle.(interface{ RequestStop(context.Context) error })
	if !ok {
		t.Fatal("managed disk handle hides the retained Tart guest shutdown request")
	}
	if err := requester.RequestStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if processHandle.requests != 1 {
		t.Fatalf("guest shutdown requests = %d, want 1", processHandle.requests)
	}
	if !held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("guest shutdown request released volume lock before exact reap")
	}
	if err := handle.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if held.MatchesScope("volume-work-" + testDiskVolume) {
		t.Fatal("volume lock retained after exact reap")
	}
}
