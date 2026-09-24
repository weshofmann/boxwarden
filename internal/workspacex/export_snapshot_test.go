package workspacex

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
)

// An interrupted host copy releases its advisory lock, but the durable
// marker must still prevent a later sandbox start or workspace detach.
func TestExportSnapshotPendingBlocksStartAndDetachAfterCopyInterruption(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	volume.Pending = &Pending{Kind: "export-snapshot", ID: testGeneration}
	if err := SaveRecord(root, domain.ID("work"), volume); err != nil {
		t.Fatal(err)
	}
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	if _, err := PrepareSessionStart(context.Background(), root, domain.ID("work"), stopped, testGeneration, observer); err == nil {
		t.Fatal("stopped sandbox started while export snapshot was pending")
	}
	if _, err := Detach(context.Background(), root, domain.ID("work"), testVolumeID, string(stopped.Name), observer); err == nil {
		t.Fatal("workspace detached while export snapshot was pending")
	}
	current, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || current.Pending == nil || *current.Pending != *volume.Pending || current.Use != nil {
		t.Fatalf("interrupted copy marker changed: %#v, %v", current, err)
	}
}

func TestExportSnapshotPendingRequiresAvailableAttachedVolume(t *testing.T) {
	root := privateRoot(t)
	creating := fixtureRecord()
	creating.Pending = &Pending{Kind: "export-snapshot", ID: testGeneration}
	if err := SaveRecord(root, domain.ID("work"), creating); err == nil {
		t.Fatal("creating workspace claimed an export copy")
	}
	available := creating
	available.State = StateAvailable
	available.Disk = &DiskIdentity{Device: 1, Inode: 2}
	if err := SaveRecord(root, domain.ID("work"), available); err == nil {
		t.Fatal("unattached workspace claimed an export copy")
	}
}
