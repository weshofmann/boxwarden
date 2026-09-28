package workspacex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func legacyReconcileFixture(t *testing.T) (string, Record, hostidentity.StorageExpectation) {
	t.Helper()
	root := privateRoot(t)
	request := managedRequest()
	record, err := CreateManaged(t.Context(), root, request, &managedFormatter{})
	if err != nil {
		t.Fatal(err)
	}
	journal, err := workspaceformat.ReadJournal(root, request)
	if err != nil {
		t.Fatal(err)
	}
	journal.Version = 1
	journal.HostIdentity = nil
	raw, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "volumes", request.VolumeID+".format.json"), append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	storage := hostidentity.StorageExpectation{ConfigPath: "/external/config.json", StateRoot: root,
		MountPoint: root, VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	return root, record, storage
}

func TestReconcileLegacyChecksBackingBeforeCreatingLock(t *testing.T) {
	root := filepath.Join(t.TempDir(), "absent", "state")
	storage := hostidentity.StorageExpectation{ConfigPath: "/external/config.json", StateRoot: root,
		MountPoint: filepath.Dir(root), VolumeUUID: "00112233-4455-6677-8899-aabbccddeeff"}
	if _, err := ReconcileLegacy(t.Context(), root, "work", testVolumeID, storage, nil); err == nil {
		t.Fatal("missing storage reached reconciliation")
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing backing root was created: %v", err)
	}
}

func TestReconcileLegacyChecksRecordAndExactStoppedAttachmentBeforeBinding(t *testing.T) {
	root, initial, storage := legacyReconcileFixture(t)
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	attached, err := Attach(t.Context(), root, domain.ID("work"), initial.VolumeID, "dev", "/home/boxwarden/workspaces/project",
		stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
	if err != nil {
		t.Fatal(err)
	}
	bound := 0
	check := func(hostidentity.StorageExpectation) error { return nil }
	bind := func(string, workspaceformat.Request, hostidentity.StorageExpectation) error {
		bound++
		return nil
	}
	if _, err := reconcileLegacy(t.Context(), root, "work", initial.VolumeID, storage,
		stoppedObserver{state: backend.ObjectRunning, object: "bw-work-dev"}, check, bind); err == nil || bound != 0 {
		t.Fatalf("running backend reached binder: calls=%d, err=%v", bound, err)
	}
	got, err := reconcileLegacy(t.Context(), root, "work", initial.VolumeID, storage,
		stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}, check, bind)
	if err != nil || bound != 1 || !reflect.DeepEqual(got, attached) {
		t.Fatalf("stopped reconciliation = %+v, calls=%d, err=%v", got, bound, err)
	}
	current, err := LoadRecord(root, "work", initial.VolumeID)
	if err != nil || !reflect.DeepEqual(current, attached) {
		t.Fatalf("historical record changed: %+v, %v", current, err)
	}
}

func TestReconcileLegacyRejectsUnresolvedUseAndPending(t *testing.T) {
	for _, kind := range []string{"use", "pending"} {
		t.Run(kind, func(t *testing.T) {
			root, record, storage := legacyReconcileFixture(t)
			writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
			attached, err := Attach(t.Context(), root, "work", record.VolumeID, "dev", "/home/boxwarden/workspaces/project",
				stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
			if err != nil {
				t.Fatal(err)
			}
			if kind == "use" {
				attached.Use = &Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}
			} else {
				attached.Pending = &Pending{Kind: "export-snapshot", ID: testGeneration}
			}
			if err := os.WriteFile(filepath.Join(root, "workspaces", record.VolumeID+".json"), []byte(mustJSON(t, attached)), 0o600); err != nil {
				t.Fatal(err)
			}
			bound := 0
			_, err = reconcileLegacy(context.Background(), root, "work", record.VolumeID, storage,
				stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"},
				func(hostidentity.StorageExpectation) error { return nil },
				func(string, workspaceformat.Request, hostidentity.StorageExpectation) error { bound++; return nil })
			if err == nil || bound != 0 {
				t.Fatalf("unresolved %s reached binder: calls=%d, err=%v", kind, bound, err)
			}
		})
	}
}

func TestReconcileLegacyRejectsRecordDifferentFromHistoricalReceipt(t *testing.T) {
	root, record, storage := legacyReconcileFixture(t)
	record.Disk.Device++
	if err := os.WriteFile(filepath.Join(root, "workspaces", record.VolumeID+".json"), []byte(mustJSON(t, record)), 0o600); err != nil {
		t.Fatal(err)
	}
	bound := 0
	_, err := reconcileLegacy(t.Context(), root, "work", record.VolumeID, storage, nil,
		func(hostidentity.StorageExpectation) error { return nil },
		func(string, workspaceformat.Request, hostidentity.StorageExpectation) error { bound++; return nil })
	if err == nil || bound != 0 {
		t.Fatalf("record mismatch reached binder: calls=%d, err=%v", bound, err)
	}
}

func TestReconcileLegacyAllowsUnattachedAvailableVolumeWithoutBackend(t *testing.T) {
	root, record, storage := legacyReconcileFixture(t)
	bound := 0
	got, err := reconcileLegacy(t.Context(), root, "work", record.VolumeID, storage, nil,
		func(hostidentity.StorageExpectation) error { return nil },
		func(string, workspaceformat.Request, hostidentity.StorageExpectation) error { bound++; return nil })
	if err != nil || bound != 1 || !reflect.DeepEqual(got, record) {
		t.Fatalf("unattached reconcile = %+v, calls=%d, err=%v", got, bound, err)
	}
}
