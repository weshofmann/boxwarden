package workspacex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func launchFixture(t *testing.T, reserve bool) (string, session.Record) {
	t.Helper()
	root := privateRoot(t)
	volume := fixtureRecord()
	volume.SizeBytes = 16 << 20
	if err := SaveRecord(root, domain.ID("work"), volume); err != nil {
		t.Fatal(err)
	}
	_, err := workspaceformat.Create(context.Background(), root, workspaceformat.Request{Domain: volume.Domain, VolumeID: volume.VolumeID, FilesystemUUID: volume.FilesystemUUID, SizeBytes: volume.SizeBytes}, formatFixture(func(_ context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
		if err := writeFixtureExt4Header(request.DiskPath, request.FilesystemUUID); err != nil {
			return workspaceformat.FormatEvidence{}, err
		}
		return workspaceformat.FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PromoteVerified(context.Background(), root, domain.ID("work"), volume.VolumeID); err != nil {
		t.Fatal(err)
	}
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	observer := stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}
	if _, err := Attach(context.Background(), root, domain.ID("work"), volume.VolumeID, "dev", "/home/boxwarden/workspaces/project", observer); err != nil {
		t.Fatal(err)
	}
	if reserve {
		if _, err := ReserveUse(context.Background(), root, domain.ID("work"), volume.VolumeID, Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}, observer); err != nil {
			t.Fatal(err)
		}
	}
	record, err := session.LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	record.IntendedState = session.StateStarting
	record.StartGeneration = testGeneration
	record.Readiness = session.ReadinessRecord{Status: session.ReadinessStarting}
	if err := session.SaveRecord(root, domain.ID("work"), record); err != nil {
		t.Fatal(err)
	}
	return root, record
}

func TestAdmitLaunchDisksRetainsExactQualifiedLease(t *testing.T) {
	root, record := launchFixture(t, true)
	set, err := AdmitLaunchDisks(context.Background(), root, domain.ID("work"), record, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
	if err != nil {
		t.Fatal(err)
	}
	request := backend.StartRequest{ObjectID: record.Backend.ObjectID, SerialDevice: "/dev/ttys001", GenerationDirectory: filepath.Join(root, "runtime", record.StartGeneration), ManagedDisks: set}
	if err := backend.ValidateStartRequest(request); err != nil {
		t.Fatalf("exact launch lease rejected: %v", err)
	}
	if err := set.CloseUnclaimed(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	held, err := AcquireVolumeUse(ctx, root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatalf("released prelaunch lease retained volume lock: %v", err)
	}
	defer held.Release()
}

func TestAdmitLaunchDisksRejectsMissingUseAndUnstoppedBackend(t *testing.T) {
	for _, test := range []struct {
		name    string
		reserve bool
		state   backend.ObjectState
	}{
		{name: "missing use", reserve: false, state: backend.ObjectStopped},
		{name: "running backend", reserve: true, state: backend.ObjectRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, record := launchFixture(t, test.reserve)
			if set, err := AdmitLaunchDisks(context.Background(), root, domain.ID("work"), record, stoppedObserver{state: test.state, object: "bw-work-dev"}); err == nil {
				if set != nil {
					_ = set.CloseUnclaimed()
				}
				t.Fatal("unsafe workspace launch lease admitted")
			}
		})
	}
}

func TestAdmitLaunchDisksRejectsReplacementRawFile(t *testing.T) {
	root, record := launchFixture(t, true)
	path := filepath.Join(root, "volumes", testVolumeID+".raw")
	if err := os.Rename(path, path+".held"); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Truncate(16 << 20); err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	if err := writeFixtureExt4Header(path, testGeneration); err != nil {
		t.Fatal(err)
	}
	if set, err := AdmitLaunchDisks(context.Background(), root, domain.ID("work"), record, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}); err == nil {
		if set != nil {
			_ = set.CloseUnclaimed()
		}
		t.Fatal("replacement raw inode admitted")
	}
}

func TestAdmitLaunchDisksRechecksSessionAfterVolumeLockWait(t *testing.T) {
	root, starting := launchFixture(t, true)
	blocker, err := AcquireVolumeUse(context.Background(), root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		set, err := AdmitLaunchDisks(ctx, root, domain.ID("work"), starting, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
		if set != nil {
			err = errors.Join(err, set.CloseUnclaimed())
		}
		done <- err
	}()
	sessionLock, err := lock.AcquireSession(ctx, root, "work", "dev")
	if err != nil {
		t.Fatalf("volume wait held session lock: %v", err)
	}
	stopping := starting
	stopping.IntendedState = session.StateStopping
	stopping.Readiness = session.ReadinessRecord{Status: session.ReadinessNotReady}
	if err := session.SaveRecord(root, domain.ID("work"), stopping); err != nil {
		t.Fatal(err)
	}
	if err := sessionLock.Release(); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Release(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("changed session intent admitted a workspace lease")
	}
}
