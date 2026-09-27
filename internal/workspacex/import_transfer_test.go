package workspacex

import (
	"context"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type importTransferFixture struct {
	snapshot supervisor.Snapshot
	transfer func(supervisor.Binding, supervisor.ImportTransfer) (supervisor.ImportResult, error)
	calls    int
}

func (f *importTransferFixture) Snapshot(_ context.Context, _ supervisor.Binding) (supervisor.Snapshot, error) {
	f.snapshot.ObservedAt = time.Now()
	return f.snapshot, nil
}
func (f *importTransferFixture) TransferImport(_ context.Context, binding supervisor.Binding, spec supervisor.ImportTransfer) (supervisor.ImportResult, error) {
	f.calls++
	return f.transfer(binding, spec)
}

func TestTransferCapturedImportHoldsSessionBindingAndLeavesUnverifiedJournal(t *testing.T) {
	root, reader := readyImportFixture(t)
	begin, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader)
	if err != nil {
		t.Fatal(err)
	}
	controller := &importTransferFixture{snapshot: reader.snapshot}
	controller.transfer = func(binding supervisor.Binding, spec supervisor.ImportTransfer) (supervisor.ImportResult, error) {
		if binding != reader.snapshot.Binding || spec.TransactionID != begin.ID || spec.VolumeID != begin.VolumeID || spec.SourceDigest != begin.SourceDigest || spec.MountPath != begin.MountPath {
			t.Fatalf("transfer binding differs: %#v %#v", binding, spec)
		}
		wait, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		defer cancel()
		if held, err := lock.AcquireSession(wait, root, "work", "dev"); err == nil {
			held.Release()
			t.Fatal("session lock was released during transfer")
		}
		storageWait, cancelStorage := context.WithTimeout(context.Background(), time.Second)
		defer cancelStorage()
		storage, err := AcquireStorageOperation(storageWait, root, domain.ID("work"))
		if err != nil {
			t.Fatalf("domain storage lock was held during long transfer: %v", err)
		}
		if err := storage.Release(); err != nil {
			t.Fatal(err)
		}
		return supervisor.ImportResult{Digest: begin.SourceDigest, FileCount: begin.FileCount, TotalBytes: begin.TotalBytes,
			RemotePath: begin.MountPath + "/boxwarden-import-" + begin.ID}, nil
	}
	result, err := TransferCapturedImport(context.Background(), root, domain.ID("work"), testImportID, controller)
	if err != nil || result.Digest != begin.SourceDigest || controller.calls != 1 {
		t.Fatalf("bound transfer = %#v, calls=%d, err=%v", result, controller.calls, err)
	}
	journal, err := LoadImportJournal(root, domain.ID("work"), testImportID)
	if err != nil || journal.Phase != ImportTransferring {
		t.Fatalf("import journal incorrectly verified: %#v, %v", journal, err)
	}
	controller.snapshot.ProbeOK = false
	if _, err := TransferCapturedImport(context.Background(), root, domain.ID("work"), testImportID, controller); err == nil || controller.calls != 1 {
		t.Fatalf("stale guest readiness reached transfer: %v, calls=%d", err, controller.calls)
	}
}

func TestTransferCapturedImportRejectsReadinessLostAfterReadback(t *testing.T) {
	root, reader := readyImportFixture(t)
	begin, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader)
	if err != nil {
		t.Fatal(err)
	}
	controller := &importTransferFixture{snapshot: reader.snapshot}
	controller.transfer = func(supervisor.Binding, supervisor.ImportTransfer) (supervisor.ImportResult, error) {
		controller.snapshot.ProbeOK = false
		return supervisor.ImportResult{Digest: begin.SourceDigest, FileCount: begin.FileCount, TotalBytes: begin.TotalBytes,
			RemotePath: begin.MountPath + "/boxwarden-import-" + begin.ID}, nil
	}
	if _, err := TransferCapturedImport(context.Background(), root, domain.ID("work"), testImportID, controller); err == nil {
		t.Fatal("readback was accepted after current generation lost readiness")
	}
}

func TestTransferCapturedImportRejectsFalseReadback(t *testing.T) {
	root, reader := readyImportFixture(t)
	begin, err := BeginImport(context.Background(), root, domain.ID("work"), "dev", testVolumeID, testImportID, reader)
	if err != nil {
		t.Fatal(err)
	}
	controller := &importTransferFixture{snapshot: reader.snapshot, transfer: func(supervisor.Binding, supervisor.ImportTransfer) (supervisor.ImportResult, error) {
		return supervisor.ImportResult{Digest: begin.SourceDigest, FileCount: begin.FileCount, TotalBytes: begin.TotalBytes + 1,
			RemotePath: begin.MountPath + "/boxwarden-import-" + begin.ID}, nil
	}}
	if _, err := TransferCapturedImport(context.Background(), root, domain.ID("work"), testImportID, controller); err == nil {
		t.Fatal("false readback was accepted")
	}
	journal, err := LoadImportJournal(root, domain.ID("work"), testImportID)
	if err != nil || journal.Phase != ImportTransferring {
		t.Fatalf("failed transfer became verified: %#v, %v", journal, err)
	}
}
