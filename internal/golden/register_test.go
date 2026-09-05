package golden

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestRegisterStoresAndLoadsObservedStoppedGolden(t *testing.T) {
	domainConfig := testDomain(t, "work")
	observer := &countingObserver{observation: backend.Observation{
		ObjectID: "golden-r1",
		Exists:   true,
		State:    backend.ObjectStopped,
	}}

	record, err := Register(context.Background(), domainConfig, "golden-r1", observer)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if got, want := record.Revision, "golden-r1"; got != want {
		t.Fatalf("Revision = %q, want %q", got, want)
	}
	if got, want := record.Backend.ObjectID, "golden-r1"; got != want {
		t.Fatalf("Backend.ObjectID = %q, want %q", got, want)
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d, want 1", observer.calls)
	}

	loaded, err := LoadCurrent(context.Background(), domainConfig)
	if err != nil {
		t.Fatalf("LoadCurrent: %v", err)
	}
	if loaded != record {
		t.Fatalf("LoadCurrent = %#v, want %#v", loaded, record)
	}
	for path, mode := range map[string]os.FileMode{
		filepath.Join(domainConfig.StateRoot, "goldens"):                              0o700,
		filepath.Join(domainConfig.StateRoot, "goldens", "records"):                   0o700,
		filepath.Join(domainConfig.StateRoot, "goldens", "records", "golden-r1.json"): 0o600,
		filepath.Join(domainConfig.StateRoot, "goldens", "current.json"):              0o600,
	} {
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("Lstat %q: %v", path, err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("mode for %q = %v, want %v", path, info.Mode().Perm(), mode)
		}
	}
}

func TestRegisterAllowsSeparateDomainAdmissionOfSameStoppedGolden(t *testing.T) {
	observer := &countingObserver{observation: backend.Observation{
		ObjectID: "golden-r1",
		Exists:   true,
		State:    backend.ObjectStopped,
	}}
	work := testDomain(t, "work")
	personal := testDomain(t, "personal")

	workRecord, err := Register(context.Background(), work, "golden-r1", observer)
	if err != nil {
		t.Fatalf("Register work: %v", err)
	}
	personalRecord, err := Register(context.Background(), personal, "golden-r1", observer)
	if err != nil {
		t.Fatalf("Register personal: %v", err)
	}
	if got, want := workRecord.Domain, domain.ID("work"); got != want {
		t.Fatalf("work record domain = %q, want %q", got, want)
	}
	if got, want := personalRecord.Domain, domain.ID("personal"); got != want {
		t.Fatalf("personal record domain = %q, want %q", got, want)
	}
	if workRecord.Backend != personalRecord.Backend {
		t.Fatalf("backend references differ: work = %#v, personal = %#v", workRecord.Backend, personalRecord.Backend)
	}
	if _, err := LoadCurrent(context.Background(), work); err != nil {
		t.Fatalf("LoadCurrent work: %v", err)
	}
	if _, err := LoadCurrent(context.Background(), personal); err != nil {
		t.Fatalf("LoadCurrent personal: %v", err)
	}
}

func TestRegisterRejectsUnacceptableObservation(t *testing.T) {
	cases := map[string]backend.Observation{
		"missing":  {ObjectID: "golden-r1", Exists: false, State: backend.ObjectUnknown},
		"running":  {ObjectID: "golden-r1", Exists: true, State: backend.ObjectRunning},
		"unknown":  {ObjectID: "golden-r1", Exists: true, State: backend.ObjectUnknown},
		"mismatch": {ObjectID: "other", Exists: true, State: backend.ObjectStopped},
	}
	for name, observation := range cases {
		t.Run(name, func(t *testing.T) {
			observer := &countingObserver{observation: observation}
			if _, err := Register(context.Background(), testDomain(t, "work"), "golden-r1", observer); err == nil {
				t.Fatal("Register error = nil, want rejection")
			}
			if observer.calls != 1 {
				t.Fatalf("observer calls = %d, want 1", observer.calls)
			}
		})
	}

	observer := &countingObserver{err: errors.New("observe failure")}
	if _, err := Register(context.Background(), testDomain(t, "work"), "golden-r1", observer); err == nil {
		t.Fatal("Register observer error = nil, want propagation")
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d, want 1", observer.calls)
	}
}

func TestRegisterRejectsInvalidTartIdentity(t *testing.T) {
	for _, name := range []string{"", "-option", "two words", "snowman-☃", "a/b"} {
		t.Run(name, func(t *testing.T) {
			observer := &countingObserver{}
			if _, err := Register(context.Background(), testDomain(t, "work"), name, observer); err == nil {
				t.Fatal("Register error = nil, want invalid identity rejection")
			}
			if observer.calls != 0 {
				t.Fatalf("observer calls = %d, want 0", observer.calls)
			}
		})
	}
}

func TestRegistrationIsIdempotentAndLeavesOnlyCompletePublicState(t *testing.T) {
	domainConfig := testDomain(t, "work")
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	first, err := Register(context.Background(), domainConfig, "golden-r1", observer)
	if err != nil {
		t.Fatalf("first Register: %v", err)
	}
	second, err := Register(context.Background(), domainConfig, "golden-r1", observer)
	if err != nil {
		t.Fatalf("second Register: %v", err)
	}
	if second != first {
		t.Fatalf("second Register = %#v, want %#v", second, first)
	}
	if observer.calls != 2 {
		t.Fatalf("observer calls = %d, want 2", observer.calls)
	}

	entries, err := os.ReadDir(filepath.Join(domainConfig.StateRoot, "goldens", "records"))
	if err != nil {
		t.Fatalf("ReadDir records: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "golden-r1.json" {
		t.Fatalf("record entries = %#v, want only complete immutable record", entries)
	}
}

func TestLoadCurrentRejectsCopiedOrUnrecognizedGoldenRecord(t *testing.T) {
	work := testDomain(t, "work")
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	if _, err := Register(context.Background(), work, "golden-r1", observer); err != nil {
		t.Fatalf("Register work: %v", err)
	}

	personal := testDomain(t, "personal")
	makeGoldenDirectories(t, personal.StateRoot)
	workRecord, err := os.ReadFile(filepath.Join(work.StateRoot, "goldens", "records", "golden-r1.json"))
	if err != nil {
		t.Fatalf("ReadFile work record: %v", err)
	}
	writePrivateFile(t, filepath.Join(personal.StateRoot, "goldens", "records", "golden-r1.json"), workRecord)
	writePrivateFile(t, filepath.Join(personal.StateRoot, "goldens", "current.json"), []byte(`{"version":1,"domain":"personal","revision":"golden-r1"}`))
	if _, err := LoadCurrent(context.Background(), personal); err == nil {
		t.Fatal("LoadCurrent copied cross-domain record error = nil, want rejection")
	}

	writePrivateFile(t, filepath.Join(work.StateRoot, "goldens", "records", "golden-r1.json"), []byte(`{"version":1,"domain":"work","revision":"golden-r1","backend":{"kind":"tart","object_id":"golden-r1"},"unexpected":true}`))
	if _, err := LoadCurrent(context.Background(), work); err == nil {
		t.Fatal("LoadCurrent record with unrecognized field error = nil, want rejection")
	}
}

func TestGoldenOperationsRejectStateAndLockSymlinks(t *testing.T) {
	target := t.TempDir()
	symlinkedState := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(target, symlinkedState); err != nil {
		t.Fatalf("Symlink state root: %v", err)
	}
	id, err := domain.Parse("work")
	if err != nil {
		t.Fatalf("Parse domain: %v", err)
	}
	observer := &countingObserver{}
	if _, err := Register(context.Background(), config.Domain{ID: id, StateRoot: symlinkedState}, "golden-r1", observer); err == nil {
		t.Fatal("Register symlinked state root error = nil, want rejection")
	}
	if observer.calls != 0 {
		t.Fatalf("observer calls = %d, want 0", observer.calls)
	}

	domainConfig := testDomain(t, "work")
	if err := os.Mkdir(filepath.Join(domainConfig.StateRoot, "locks"), 0o700); err != nil {
		t.Fatalf("Mkdir locks: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(domainConfig.StateRoot, "locks", "golden.lock")); err != nil {
		t.Fatalf("Symlink golden lock: %v", err)
	}
	if _, err := Register(context.Background(), domainConfig, "golden-r1", observer); err == nil {
		t.Fatal("Register symlinked golden lock error = nil, want rejection")
	}
}

func TestRegisterRejectsConflictingImmutableRevision(t *testing.T) {
	domainConfig := testDomain(t, "work")
	makeGoldenDirectories(t, domainConfig.StateRoot)
	writePrivateFile(t, filepath.Join(domainConfig.StateRoot, "goldens", "records", "golden-r1.json"), []byte(`{"version":1,"domain":"personal","revision":"golden-r1","backend":{"kind":"tart","object_id":"golden-r1"}}`))
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	if _, err := Register(context.Background(), domainConfig, "golden-r1", observer); err == nil {
		t.Fatal("Register conflicting immutable revision error = nil, want rejection")
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d, want 1", observer.calls)
	}
}

func TestLoadRevisionLockedRetainsSelectedImmutableGoldenAfterCurrentChanges(t *testing.T) {
	domainConfig := testDomain(t, "work")
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	first, err := Register(context.Background(), domainConfig, "golden-r1", observer)
	if err != nil {
		t.Fatalf("Register golden-r1: %v", err)
	}
	observer.observation.ObjectID = "golden-r2"
	if _, err := Register(context.Background(), domainConfig, "golden-r2", observer); err != nil {
		t.Fatalf("Register golden-r2: %v", err)
	}

	held, err := AcquireLock(context.Background(), domainConfig)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	defer held.Release()
	loaded, err := LoadRevisionLocked(domainConfig, first.Revision)
	if err != nil {
		t.Fatalf("LoadRevisionLocked golden-r1: %v", err)
	}
	if loaded != first {
		t.Fatalf("LoadRevisionLocked = %#v, want %#v", loaded, first)
	}
}

func TestRegisterSyncsEachNewGoldenDirectoryParent(t *testing.T) {
	domainConfig := testDomain(t, "work")
	oldSync := syncRoot
	defer func() { syncRoot = oldSync }()
	calls := map[string]int{}
	syncRoot = func(root *os.Root) error {
		calls[filepath.Base(root.Name())]++
		return nil
	}
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	if _, err := Register(context.Background(), domainConfig, "golden-r1", observer); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if calls[filepath.Base(domainConfig.StateRoot)] != 1 || calls["goldens"] != 2 || calls["records"] != 1 {
		t.Fatalf("directory sync calls = %#v, want state root once, goldens twice, records once", calls)
	}
}

func TestRegisterFailsClosedWhenValidatedGoldenDirectoryIsReplaced(t *testing.T) {
	domainConfig := testDomain(t, "work")
	makeGoldenDirectories(t, domainConfig.StateRoot)
	goldens := filepath.Join(domainConfig.StateRoot, "goldens")
	oldHook := beforeOpenChild
	defer func() { beforeOpenChild = oldHook }()
	beforeOpenChild = func(parent *os.Root, name string) {
		if name != "goldens" {
			return
		}
		if err := os.Rename(goldens, filepath.Join(domainConfig.StateRoot, "goldens-original")); err != nil {
			t.Fatalf("Rename validated goldens: %v", err)
		}
		if err := os.Mkdir(goldens, 0o700); err != nil {
			t.Fatalf("Mkdir replacement goldens: %v", err)
		}
	}
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	if _, err := Register(context.Background(), domainConfig, "golden-r1", observer); err == nil {
		t.Fatal("Register golden directory replacement error = nil, want closed failure")
	}
	if _, err := os.Lstat(filepath.Join(goldens, "records", "golden-r1.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("replacement golden directory unexpectedly used: %v", err)
	}
}

func TestLoadCurrentRejectsDuplicateGoldenFieldsAndOversizedWhitespace(t *testing.T) {
	domainConfig := testDomain(t, "work")
	observer := &countingObserver{observation: backend.Observation{ObjectID: "golden-r1", Exists: true, State: backend.ObjectStopped}}
	if _, err := Register(context.Background(), domainConfig, "golden-r1", observer); err != nil {
		t.Fatalf("Register: %v", err)
	}
	recordPath := filepath.Join(domainConfig.StateRoot, "goldens", "records", "golden-r1.json")
	valid := `{"version":1,"domain":"work","revision":"golden-r1","backend":{"kind":"tart","object_id":"golden-r1"}}`
	for name, contents := range map[string]string{
		"top-level": `{"version":1,"version":1,"domain":"work","revision":"golden-r1","backend":{"kind":"tart","object_id":"golden-r1"}}`,
		"backend":   `{"version":1,"domain":"work","revision":"golden-r1","backend":{"kind":"tart","kind":"tart","object_id":"golden-r1"}}`,
		"oversized": valid + string(make([]byte, (1<<20)-len(valid)+1)),
	} {
		t.Run(name, func(t *testing.T) {
			contents = strings.ReplaceAll(contents, "\x00", " ")
			writePrivateFile(t, recordPath, []byte(contents))
			if _, err := LoadCurrent(context.Background(), domainConfig); err == nil {
				t.Fatal("LoadCurrent error = nil, want rejection")
			}
		})
	}

	writePrivateFile(t, recordPath, []byte(valid))
	writePrivateFile(t, filepath.Join(domainConfig.StateRoot, "goldens", "current.json"), []byte(`{"version":1,"version":1,"domain":"work","revision":"golden-r1"}`))
	if _, err := LoadCurrent(context.Background(), domainConfig); err == nil {
		t.Fatal("LoadCurrent duplicate pointer error = nil, want rejection")
	}
}

type countingObserver struct {
	observation backend.Observation
	err         error
	calls       int
}

func (o *countingObserver) Observe(_ context.Context, _ string) (backend.Observation, error) {
	o.calls++
	if o.err != nil {
		return backend.Observation{}, o.err
	}
	return o.observation, nil
}

func testDomain(t *testing.T, raw string) config.Domain {
	t.Helper()
	id, err := domain.Parse(raw)
	if err != nil {
		t.Fatalf("Parse domain: %v", err)
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks root: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("Chmod root: %v", err)
	}
	return config.Domain{ID: id, StateRoot: root}
}

func makeGoldenDirectories(t *testing.T, root string) {
	t.Helper()
	for _, path := range []string{filepath.Join(root, "goldens"), filepath.Join(root, "goldens", "records")} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("Mkdir %q: %v", path, err)
		}
	}
}

func writePrivateFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile %q: %v", path, err)
	}
}
