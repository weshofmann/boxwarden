package session

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestSaveRecordAtomicallyCreatesOwnerOnlyState(t *testing.T) {
	stateRoot := privateStateRoot(t)
	record := storeTestRecord(StateCreating)

	if err := SaveRecord(stateRoot, record.Domain, record); err != nil {
		t.Fatalf("SaveRecord() error = %v", err)
	}

	sessionsInfo, err := os.Lstat(filepath.Join(stateRoot, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if sessionsInfo.Mode().Perm() != 0o700 {
		t.Fatalf("sessions mode = %04o, want 0700", sessionsInfo.Mode().Perm())
	}
	recordInfo, err := os.Lstat(filepath.Join(stateRoot, "sessions", "dev.json"))
	if err != nil {
		t.Fatal(err)
	}
	if recordInfo.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %04o, want 0600", recordInfo.Mode().Perm())
	}
	loaded, err := LoadRecord(stateRoot, "work", "dev")
	if err != nil {
		t.Fatalf("LoadRecord() error = %v", err)
	}
	if loaded != record {
		t.Fatalf("LoadRecord() = %#v, want %#v", loaded, record)
	}
	entries, err := os.ReadDir(filepath.Join(stateRoot, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "dev.json" {
		t.Fatalf("session directory entries = %v, want only dev.json", entryNames(entries))
	}
}

func TestSaveRecordAtomicallyUpgradesStoppedVersion1RecordWithoutChangingIdentity(t *testing.T) {
	stateRoot := privateStateRoot(t)
	if err := os.Mkdir(filepath.Join(stateRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeRecord(t, stateRoot, "dev", `{"version":1,"domain":"work","name":"dev","id":"00000000-0000-4000-8000-000000000001","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-work-r1"}`)
	legacy, err := LoadRecord(stateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}

	if err := SaveRecord(stateRoot, legacy.Domain, legacy); err != nil {
		t.Fatalf("SaveRecord() error = %v", err)
	}
	upgraded, err := LoadRecord(stateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	want := version2NotReadyRecord(legacy)
	if upgraded != want {
		t.Fatalf("upgraded record = %#v, want %#v", upgraded, want)
	}
	if upgraded.Domain != legacy.Domain || upgraded.Name != legacy.Name || upgraded.ID != legacy.ID || upgraded.Mode != legacy.Mode || upgraded.IntendedState != legacy.IntendedState || upgraded.Backend != legacy.Backend || upgraded.GoldenRevision != legacy.GoldenRevision {
		t.Fatalf("upgrade changed legacy identity: got %#v, legacy %#v", upgraded, legacy)
	}
}

func TestSaveRecordRejectsVersion1RecordWithVersion2Fields(t *testing.T) {
	stateRoot := privateStateRoot(t)
	legacy := storeTestRecord(StateStopped)
	legacy.Version = recordVersionV1
	legacy.Readiness = ReadinessRecord{Status: ReadinessReady}
	legacy.StartGeneration = "00112233-4455-4677-8899-aabbccddeeff"

	if err := SaveRecord(stateRoot, legacy.Domain, legacy); err == nil {
		t.Fatal("SaveRecord() error = nil, want version-mixing rejection")
	}
}

func TestSaveRecordUpgradeFaultBoundariesKeepOneCompleteVersion(t *testing.T) {
	for name, hook := range map[string]storeHook{
		"before rename retains version 1": func(stage storeStage) error {
			if stage == storeBeforeRename {
				return errors.New("interrupted before rename")
			}
			return nil
		},
		"after rename exposes version 2": func(stage storeStage) error {
			if stage == storeAfterRename {
				return errors.New("interrupted after rename")
			}
			return nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			stateRoot := privateStateRoot(t)
			if err := os.Mkdir(filepath.Join(stateRoot, "sessions"), 0o700); err != nil {
				t.Fatal(err)
			}
			writeRecord(t, stateRoot, "dev", `{"version":1,"domain":"work","name":"dev","id":"00000000-0000-4000-8000-000000000001","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-work-r1"}`)
			legacy, err := LoadRecord(stateRoot, "work", "dev")
			if err != nil {
				t.Fatal(err)
			}
			if err := saveRecord(stateRoot, legacy.Domain, legacy, hook); err == nil {
				t.Fatal("saveRecord() error = nil, want simulated interruption")
			}
			loaded, err := LoadRecord(stateRoot, "work", "dev")
			if err != nil {
				t.Fatal(err)
			}
			if name == "before rename retains version 1" && loaded != legacy {
				t.Fatalf("pre-rename record = %#v, want legacy %#v", loaded, legacy)
			}
			if name == "after rename exposes version 2" && loaded != version2NotReadyRecord(legacy) {
				t.Fatalf("post-rename record = %#v, want upgraded %#v", loaded, version2NotReadyRecord(legacy))
			}
			assertNoTemporaryRecords(t, stateRoot)
		})
	}
}

func TestSaveRecordPreRenameFaultPreservesPreviousCompleteRecord(t *testing.T) {
	stateRoot := privateStateRoot(t)
	original := storeTestRecord(StateCreating)
	if err := SaveRecord(stateRoot, original.Domain, original); err != nil {
		t.Fatal(err)
	}

	replacement := original
	replacement.IntendedState = StateStopped
	wantErr := errors.New("simulated interruption")
	err := saveRecord(stateRoot, replacement.Domain, replacement, func(stage storeStage) error {
		if stage == storeBeforeRename {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("saveRecord() error = %v, want simulated interruption", err)
	}
	loaded, err := LoadRecord(stateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != original {
		t.Fatalf("record after pre-rename fault = %#v, want original %#v", loaded, original)
	}
	assertNoTemporaryRecords(t, stateRoot)
}

func TestSaveRecordPostRenameFaultLeavesNewCompleteRecordForRetry(t *testing.T) {
	stateRoot := privateStateRoot(t)
	original := storeTestRecord(StateCreating)
	if err := SaveRecord(stateRoot, original.Domain, original); err != nil {
		t.Fatal(err)
	}

	replacement := original
	replacement.IntendedState = StateStopped
	wantErr := errors.New("simulated directory sync failure")
	err := saveRecord(stateRoot, replacement.Domain, replacement, func(stage storeStage) error {
		if stage == storeAfterRename {
			return wantErr
		}
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("saveRecord() error = %v, want simulated directory sync failure", err)
	}
	loaded, err := LoadRecord(stateRoot, "work", "dev")
	if err != nil {
		t.Fatal(err)
	}
	if loaded != replacement {
		t.Fatalf("record after post-rename fault = %#v, want replacement %#v", loaded, replacement)
	}
	if err := SaveRecord(stateRoot, replacement.Domain, replacement); err != nil {
		t.Fatalf("idempotent retry error = %v", err)
	}
	assertNoTemporaryRecords(t, stateRoot)
}

func TestSaveRecordRejectsInvalidOwnershipAndSymlinkedState(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, stateRoot string, record *Record){
		"wrong domain": func(_ *testing.T, _ string, record *Record) {
			record.Domain = domain.ID("personal")
		},
		"invalid record": func(_ *testing.T, _ string, record *Record) {
			record.Backend.ObjectID = "--delete"
		},
		"symlinked sessions": func(t *testing.T, stateRoot string, _ *Record) {
			t.Helper()
			target := t.TempDir()
			if err := os.Symlink(target, filepath.Join(stateRoot, "sessions")); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			stateRoot := privateStateRoot(t)
			record := storeTestRecord(StateCreating)
			mutate(t, stateRoot, &record)
			if err := SaveRecord(stateRoot, domain.ID("work"), record); err == nil {
				t.Fatal("SaveRecord() error = nil, want rejection")
			}
		})
	}
}

func TestSaveRecordSyncsStateRootWhenCreatingSessionDirectory(t *testing.T) {
	stateRoot := privateStateRoot(t)
	oldSync := sessionSyncRoot
	defer func() { sessionSyncRoot = oldSync }()
	calls := 0
	sessionSyncRoot = func(_ *os.Root) error {
		calls++
		return nil
	}
	if err := SaveRecord(stateRoot, domain.ID("work"), storeTestRecord(StateCreating)); err != nil {
		t.Fatalf("SaveRecord() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("directory sync calls = %d, want state root after sessions create and sessions after record rename", calls)
	}
}

func TestSaveRecordFailsClosedWhenValidatedSessionDirectoryIsReplaced(t *testing.T) {
	stateRoot := privateStateRoot(t)
	sessions := filepath.Join(stateRoot, "sessions")
	if err := os.Mkdir(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	oldHook := sessionBeforeOpenChild
	defer func() { sessionBeforeOpenChild = oldHook }()
	sessionBeforeOpenChild = func(_ *os.Root, name string) {
		if name != "sessions" {
			return
		}
		if err := os.Rename(sessions, filepath.Join(stateRoot, "sessions-original")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(sessions, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveRecord(stateRoot, domain.ID("work"), storeTestRecord(StateCreating)); err == nil {
		t.Fatal("SaveRecord() error = nil, want directory replacement rejection")
	}
	if _, err := os.Lstat(filepath.Join(sessions, "dev.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement session directory unexpectedly used: %v", err)
	}
}

func TestSaveRecordRejectsGroupReadableExistingTarget(t *testing.T) {
	stateRoot := privateStateRoot(t)
	record := storeTestRecord(StateCreating)
	if err := SaveRecord(stateRoot, record.Domain, record); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stateRoot, "sessions", "dev.json")
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	record.IntendedState = StateStopped
	if err := SaveRecord(stateRoot, record.Domain, record); err == nil {
		t.Fatal("SaveRecord() error = nil, want unsafe existing target rejection")
	}
}

func privateStateRoot(t *testing.T) string {
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

func storeTestRecord(state IntendedState) Record {
	return Record{
		Version:       recordVersion,
		Domain:        domain.ID("work"),
		Name:          Name("dev"),
		ID:            "00000000-0000-4000-8000-000000000001",
		Mode:          ModeClean,
		IntendedState: state,
		Backend: BackendRef{
			Kind:     "tart",
			ObjectID: "boxwarden-work-dev",
		},
		GoldenRevision: "golden-work-r1",
		Readiness:      ReadinessRecord{Status: ReadinessNotReady},
	}
}

func version2NotReadyRecord(record Record) Record {
	record.Version = recordVersion
	record.StartGeneration = ""
	record.Readiness = ReadinessRecord{Status: ReadinessNotReady}
	return record
}

func assertNoTemporaryRecords(t *testing.T, stateRoot string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(stateRoot, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".tmp-") {
			t.Fatalf("temporary record remains after failure: %s", entry.Name())
		}
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
