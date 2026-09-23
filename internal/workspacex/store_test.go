package workspacex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	testSessionID  = "13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0"
	testVolumeID   = "00112233-4455-4677-8899-aabbccddeeff"
	testGeneration = "2d8fa24b-6163-4a64-9c63-693abf977929"
)

func TestSaveLoadRecordPreservesOneDomainSessionVolumeBinding(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	if err := SaveRecord(root, domain.ID("work"), record); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	got, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if got != record {
		t.Fatalf("LoadRecord = %#v, want %#v", got, record)
	}
	if _, err := LoadRecord(root, domain.ID("personal"), testVolumeID); err == nil {
		t.Fatal("cross-domain load accepted")
	}
	if _, err := os.Stat(filepath.Join(root, "workspaces", testVolumeID+".json")); err != nil {
		t.Fatalf("volume-keyed record absent: %v", err)
	}
}

func TestLoadRecordRejectsAmbiguousOrUnsafeState(t *testing.T) {
	valid := `{"version":1,"domain":"work","volume_id":"` + testVolumeID + `","size_bytes":4096,"format":"raw-ext4","filesystem_uuid":"` + testGeneration + `","state":"creating"}`
	cases := map[string]string{
		"duplicate field":     strings.Replace(valid, `"state":"creating"`, `"state":"creating","state":"failed"`, 1),
		"unknown field":       valid[:len(valid)-1] + `,"extra":true}`,
		"invalid filesystem":  strings.Replace(valid, testGeneration, "../escape", 1),
		"invalid volume":      strings.Replace(valid, testVolumeID, "../escape", 1),
		"unsupported version": strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"invalid state":       strings.Replace(valid, `"creating"`, `"ready"`, 1),
		"invalid size":        strings.Replace(valid, `4096`, `4095`, 1),
		"missing volume":      strings.Replace(valid, `,"volume_id":"`+testVolumeID+`"`, "", 1),
		"trailing object":     valid + `{}`,
		"creating with use":   valid[:len(valid)-1] + `,"use":{"backend_kind":"tart","backend_object":"bw-work-dev","generation":"` + testGeneration + `"}}`,
		"available no disk":   strings.Replace(valid, `"creating"`, `"available"`, 1),
		"nested unknown":      strings.Replace(valid, `"creating"`, `"available","disk":{"device":1,"inode":2,"extra":3}`, 1),
		"nested duplicate":    strings.Replace(valid, `"creating"`, `"available","disk":{"device":1,"device":2,"inode":3}`, 1),
	}
	for label, raw := range cases {
		t.Run(label, func(t *testing.T) {
			root := privateRoot(t)
			writeRawRecord(t, root, raw)
			if _, err := LoadRecord(root, domain.ID("work"), testVolumeID); err == nil {
				t.Fatal("LoadRecord accepted malformed record")
			}
		})
	}
}

func TestLoadRecordRejectsSymlinksHardlinksAndGroupAccess(t *testing.T) {
	valid := `{"version":1,"domain":"work","volume_id":"` + testVolumeID + `","size_bytes":4096,"format":"raw-ext4","filesystem_uuid":"` + testGeneration + `","state":"creating"}`
	for _, kind := range []string{"root symlink", "directory symlink", "record symlink", "record hardlink", "group directory", "group record"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			writeRawRecord(t, root, valid)
			path := filepath.Join(root, "workspaces", testVolumeID+".json")
			switch kind {
			case "root symlink":
				alias := filepath.Join(t.TempDir(), "alias")
				if err := os.Symlink(root, alias); err != nil {
					t.Fatal(err)
				}
				root = alias
			case "directory symlink":
				original := filepath.Join(root, "original")
				if err := os.Rename(filepath.Join(root, "workspaces"), original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, filepath.Join(root, "workspaces")); err != nil {
					t.Fatal(err)
				}
			case "record symlink":
				original := filepath.Join(root, "original.json")
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			case "record hardlink":
				if err := os.Link(path, filepath.Join(root, "other.json")); err != nil {
					t.Fatal(err)
				}
			case "group directory":
				if err := os.Chmod(filepath.Join(root, "workspaces"), 0o750); err != nil {
					t.Fatal(err)
				}
			case "group record":
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := LoadRecord(root, domain.ID("work"), testVolumeID); err == nil {
				t.Fatal("LoadRecord accepted unsafe path")
			}
		})
	}
}

func TestSaveRecordReplacesAtomically(t *testing.T) {
	root := privateRoot(t)
	first := fixtureRecord()
	if err := SaveRecord(root, domain.ID("work"), first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.State = StateFailed
	err := saveRecord(root, domain.ID("work"), second, func(stage storeStage) error {
		if stage == storeBeforeRename {
			return errors.New("injected failure")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected injected failure")
	}
	got, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if got != first {
		t.Fatalf("partial replacement visible: %#v", got)
	}
	entries, err := os.ReadDir(filepath.Join(root, "workspaces"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temporary record retained: %v", entries)
	}
}

func TestSaveRecordKeepsVolumeIdentityImmutableAndAllowsSecondVolume(t *testing.T) {
	root := privateRoot(t)
	first := fixtureRecord()
	if err := SaveRecord(root, domain.ID("work"), first); err != nil {
		t.Fatal(err)
	}
	changed := first
	changed.FilesystemUUID = "11335577-99bb-4cdd-8eff-0123456789ab"
	if err := SaveRecord(root, domain.ID("work"), changed); err == nil {
		t.Fatal("filesystem identity changed")
	}
	second := first
	second.VolumeID = "7fb25db7-3cc1-4d92-a04c-b60fd05fa421"
	if err := SaveRecord(root, domain.ID("work"), second); err != nil {
		t.Fatalf("independent unattached volume rejected: %v", err)
	}
	got, err := LoadRecord(root, domain.ID("work"), first.VolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if got != first {
		t.Fatalf("original binding changed: %#v", got)
	}
}

func TestSaveRecordDoesNotReopenFailedVolumeOrRecreateAvailableVolume(t *testing.T) {
	root := privateRoot(t)
	creating := fixtureRecord()
	if err := SaveRecord(root, domain.ID("work"), creating); err != nil {
		t.Fatal(err)
	}
	failed := creating
	failed.State = StateFailed
	if err := SaveRecord(root, domain.ID("work"), failed); err != nil {
		t.Fatal(err)
	}
	reopened := failed
	reopened.State = StateAvailable
	reopened.Disk = &DiskIdentity{Device: 1, Inode: 2}
	if err := SaveRecord(root, domain.ID("work"), reopened); err == nil {
		t.Fatal("failed volume silently reopened")
	}

	otherRoot := privateRoot(t)
	available := fixtureRecord()
	available.State = StateAvailable
	available.Disk = &DiskIdentity{Device: 1, Inode: 2}
	writeRawRecord(t, otherRoot, mustJSON(t, available))
	recreate := fixtureRecord()
	if err := SaveRecord(otherRoot, domain.ID("work"), recreate); err == nil {
		t.Fatal("available volume returned to creating")
	}
}

func TestSaveRecordCannotReplaceOrClearExactUseReservation(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.State = StateAvailable
	record.Disk = &DiskIdentity{Device: 1, Inode: 2}
	record.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project"}
	record.Use = &Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}
	writeRawRecord(t, root, mustJSON(t, record))
	got, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Use == nil || *got.Use != *record.Use {
		t.Fatalf("durable use = %#v, want %#v", got.Use, record.Use)
	}
	cleared := record
	cleared.Use = nil
	if err := SaveRecord(root, domain.ID("work"), cleared); err == nil {
		t.Fatal("cleared use without backend stop proof")
	}
	replaced := record
	replaced.Use = &Use{BackendKind: "tart", BackendObject: "bw-work-other", Generation: testGeneration}
	if err := SaveRecord(root, domain.ID("work"), replaced); err == nil {
		t.Fatal("replaced exact use reservation")
	}
	for label, invalid := range map[string]Use{
		"wrong backend":      {BackendKind: "qemu", BackendObject: "bw-work-dev", Generation: testGeneration},
		"unsafe object":      {BackendKind: "tart", BackendObject: "../other", Generation: testGeneration},
		"invalid generation": {BackendKind: "tart", BackendObject: "bw-work-dev", Generation: "latest"},
	} {
		t.Run(label, func(t *testing.T) {
			changed := record
			changed.Use = &invalid
			if err := SaveRecord(root, domain.ID("work"), changed); err == nil {
				t.Fatal("invalid use saved")
			}
		})
	}
}

func TestInspectManagedDiskRequiresExactFileSizeAndIdentity(t *testing.T) {
	root := privateRoot(t)
	path, err := ManagedDiskPath(root, testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := InspectManagedDisk(root, testVolumeID, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 8192); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectManagedDisk(root, testVolumeID, 4096); err == nil {
		t.Fatal("inspected resized disk as expected size")
	}
	// Keep the old inode allocated so replacement identity cannot pass by inode
	// reuse on a fast local filesystem.
	if err := os.Link(path, filepath.Join(root, "held-old.raw")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	replaced, err := InspectManagedDisk(root, testVolumeID, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if replaced == identity {
		t.Fatal("replacement file kept original identity")
	}
}

func TestManagedDiskPathRejectsSymlinkedVolumeDirectory(t *testing.T) {
	root := privateRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "volumes")); err != nil {
		t.Fatal(err)
	}
	if _, err := ManagedDiskPath(root, testVolumeID); err == nil {
		t.Fatal("returned path through symlink")
	}
}

func TestInspectManagedDiskRejectsSymlinkHardlinkAndNonprivateFile(t *testing.T) {
	for _, kind := range []string{"volume directory symlink", "disk symlink", "disk hardlink", "group disk"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			path, err := ManagedDiskPath(root, testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "volume directory symlink":
				original := filepath.Join(root, "original")
				if err := os.Rename(filepath.Dir(path), original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			case "disk symlink":
				original := filepath.Join(root, "original.raw")
				if err := os.Rename(path, original); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(original, path); err != nil {
					t.Fatal(err)
				}
			case "disk hardlink":
				if err := os.Link(path, filepath.Join(root, "other.raw")); err != nil {
					t.Fatal(err)
				}
			case "group disk":
				if err := os.Chmod(path, 0o640); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := InspectManagedDisk(root, testVolumeID, 4096); err == nil {
				t.Fatal("unsafe disk admitted")
			}
		})
	}
}

func TestVolumeUseLockContentionDoesNotSubstituteForDurableReservation(t *testing.T) {
	root := privateRoot(t)
	first, err := AcquireVolumeUse(context.Background(), root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := AcquireVolumeUse(ctx, root, domain.ID("work"), testVolumeID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same volume lock error = %v", err)
	}
	other, err := AcquireVolumeUse(context.Background(), root, domain.ID("work"), "11335577-99bb-4cdd-8eff-0123456789ab")
	if err != nil {
		t.Fatalf("independent volume blocked: %v", err)
	}
	other.Release()
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := AcquireVolumeUse(context.Background(), root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
}

func TestStorageOperationLockSerializesDomain(t *testing.T) {
	root := privateRoot(t)
	first, err := AcquireStorageOperation(context.Background(), root, domain.ID("work"))
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	if _, err := AcquireStorageOperation(ctx, root, domain.ID("work")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("same domain lock error = %v", err)
	}
	if _, err := AcquireStorageOperation(context.Background(), root, domain.ID("../evil")); err == nil {
		t.Fatal("invalid domain acquired lock")
	}
}

func fixtureRecord() Record {
	return Record{Version: 1, Domain: domain.ID("work"), VolumeID: testVolumeID, SizeBytes: 4096, Format: FormatRawExt4, FilesystemUUID: testGeneration, State: StateCreating}
}

func privateRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeRawRecord(t *testing.T, root, raw string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, "workspaces"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspaces", testVolumeID+".json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}
