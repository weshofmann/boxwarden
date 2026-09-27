package workspacex

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

type postEffectDelete struct {
	backend *fake.Backend
	err     error
}

func (d postEffectDelete) Delete(ctx context.Context, objectID string) error {
	if err := d.backend.Delete(ctx, objectID); err != nil {
		return err
	}
	return d.err
}

func deletionFixture(t *testing.T) (string, session.Record, *fake.Backend) {
	t.Helper()
	root, stopped := stoppedLaunchFixture(t)
	backendFake := fake.New(backend.Observation{ObjectID: stopped.Backend.ObjectID, Exists: true, State: backend.ObjectStopped})
	return root, stopped, backendFake
}

func deletionService(root string, observer *fake.Backend, deleter backend.Deleter) *session.DeleteService {
	return session.NewDeleteService(config.Domain{ID: domain.ID("work"), StateRoot: root}, session.DeleteDependencies{
		Observer: observer, Deleter: deleter, Gate: WithStoppedDeleteGate, Finalize: FinalizeDeletedSession,
	})
}

func TestDeleteStoppedSessionRetainsExactWorkspaceBytes(t *testing.T) {
	root, stopped, backendFake := deletionFixture(t)
	addSecondQualifiedVolume(t, root)
	disks := make(map[string][32]byte)
	for _, volumeID := range []string{testVolumeID, secondVolumeID} {
		path := filepath.Join(root, "volumes", volumeID+".raw")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		disks[volumeID] = sha256.Sum256(raw)
	}
	if err := deletionService(root, backendFake, backendFake).Delete(context.Background(), "dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.LoadRecord(root, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted session record remains: %v", err)
	}
	if calls := backendFake.DeleteCalls(); len(calls) != 1 || calls[0] != stopped.Backend.ObjectID {
		t.Fatalf("backend delete calls = %v", calls)
	}
	for volumeID, digest := range disks {
		volume, err := LoadRecord(root, domain.ID("work"), volumeID)
		if err != nil || volume.Attachment != nil || volume.Use != nil || volume.Pending != nil || volume.State != StateAvailable {
			t.Fatalf("retained volume %s = %#v, %v", volumeID, volume, err)
		}
		raw, err := os.ReadFile(filepath.Join(root, "volumes", volumeID+".raw"))
		if err != nil || sha256.Sum256(raw) != digest {
			t.Fatalf("retained disk %s changed: %v", volumeID, err)
		}
	}
}

func TestDeleteRetryAfterBackendPostEffectFailure(t *testing.T) {
	root, stopped, backendFake := deletionFixture(t)
	injected := errors.New("delete completed but reply failed")
	service := deletionService(root, backendFake, postEffectDelete{backend: backendFake, err: injected})
	if err := service.Delete(context.Background(), "dev"); !errors.Is(err, injected) {
		t.Fatalf("post-effect error = %v", err)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current.ID != stopped.ID || current.IntendedState != session.StateDeleting {
		t.Fatalf("deletion intent = %#v, %v", current, err)
	}
	volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Attachment == nil || volume.Attachment.SessionID != stopped.ID {
		t.Fatalf("attachment released before deletion reconciled: %#v, %v", volume, err)
	}
	if err := deletionService(root, backendFake, backendFake).Delete(context.Background(), "dev"); err != nil {
		t.Fatalf("deletion retry = %v", err)
	}
	if len(backendFake.DeleteCalls()) != 1 {
		t.Fatalf("absent backend was re-deleted: %v", backendFake.DeleteCalls())
	}
	volume, err = LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || volume.Attachment != nil {
		t.Fatalf("retained volume not detached on retry: %#v, %v", volume, err)
	}
}

func TestDeleteRetrySyncsPartialTwoVolumeDetachBeforeRemovingSession(t *testing.T) {
	root, _, backendFake := deletionFixture(t)
	addSecondQualifiedVolume(t, root)
	injected := errors.New("workspace directory sync failed after rename")
	originalSync := syncWorkspaceRecordDirectory
	defer func() { syncWorkspaceRecordDirectory = originalSync }()
	fail := true
	syncWorkspaceRecordDirectory = func(directory *os.Root) error {
		if fail {
			fail = false
			return injected
		}
		return originalSync(directory)
	}
	service := deletionService(root, backendFake, backendFake)
	if err := service.Delete(context.Background(), "dev"); !errors.Is(err, injected) {
		t.Fatalf("partial detach error = %v", err)
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current.IntendedState != session.StateDeleting {
		t.Fatalf("session lost deleting intent: %#v, %v", current, err)
	}
	first, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil || first.Attachment != nil {
		t.Fatalf("first visible detach = %#v, %v", first, err)
	}
	second, err := LoadRecord(root, domain.ID("work"), secondVolumeID)
	if err != nil || second.Attachment == nil {
		t.Fatalf("second attachment changed: %#v, %v", second, err)
	}
	if err := service.Delete(context.Background(), "dev"); err != nil {
		t.Fatalf("partial detach retry = %v", err)
	}
	if len(backendFake.DeleteCalls()) != 1 {
		t.Fatalf("backend delete repeated: %v", backendFake.DeleteCalls())
	}
	if _, err := session.LoadRecord(root, "work", "dev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("session record remained after durable detaches: %v", err)
	}
	for _, volumeID := range []string{testVolumeID, secondVolumeID} {
		volume, err := LoadRecord(root, domain.ID("work"), volumeID)
		if err != nil || volume.Attachment != nil {
			t.Fatalf("volume %s remained attached: %#v, %v", volumeID, volume, err)
		}
	}
}

func TestDeleteRejectsMissingRetainedDiskBeforeBackendMutation(t *testing.T) {
	root, stopped, backendFake := deletionFixture(t)
	if err := os.Remove(filepath.Join(root, "volumes", testVolumeID+".raw")); err != nil {
		t.Fatal(err)
	}
	if err := deletionService(root, backendFake, backendFake).Delete(context.Background(), "dev"); err == nil {
		t.Fatal("missing retained disk admitted for deletion")
	}
	current, err := session.LoadRecord(root, "work", "dev")
	if err != nil || current != stopped || len(backendFake.DeleteCalls()) != 0 {
		t.Fatalf("missing disk changed session/backend: %#v, %v, %v", current, err, backendFake.DeleteCalls())
	}
}

func TestDeleteRejectsRunningAndBusyWorkspaceWithoutIntent(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, session.Record, *fake.Backend)
	}{
		{name: "running backend", mutate: func(_ *testing.T, _ string, record session.Record, backendFake *fake.Backend) {
			backendFake.SetObservation(backend.Observation{ObjectID: record.Backend.ObjectID, Exists: true, State: backend.ObjectRunning})
		}},
		{name: "busy volume", mutate: func(t *testing.T, root string, record session.Record, backendFake *fake.Backend) {
			_, err := ReserveUse(context.Background(), root, domain.ID("work"), testVolumeID,
				Use{BackendKind: "tart", BackendObject: record.Backend.ObjectID, Generation: testGeneration}, backendFake)
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, stopped, backendFake := deletionFixture(t)
			test.mutate(t, root, stopped, backendFake)
			if err := deletionService(root, backendFake, backendFake).Delete(context.Background(), "dev"); err == nil {
				t.Fatal("unsafe deletion was admitted")
			}
			current, err := session.LoadRecord(root, "work", "dev")
			if err != nil || current != stopped || len(backendFake.DeleteCalls()) != 0 {
				t.Fatalf("unsafe deletion changed session or backend: %#v, %v, %v", current, err, backendFake.DeleteCalls())
			}
		})
	}
}
