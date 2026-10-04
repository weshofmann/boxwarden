package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
	"github.com/weshofmann/boxwarden/internal/workspacex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/hostidentity"
)

// A missing route, accidental acceptance of a malformed name/size, or omitted
// backing-store admission breaks these public contracts.
func TestProjectCommandsAcceptOperatorInputsWithoutInternalIDs(t *testing.T) {
	for _, command := range [][]string{
		{"project", "setup", "--source-root", "/source", "--formatter-bundle", "/formatter", "--iso", "/ubuntu.iso", "--go", "/tool/go"},
		{"project", "create", "--base", "current", "--size-mib", "64", "demo"},
		{"project", "open", "demo"}, {"project", "status", "demo"}, {"project", "stop", "demo"},
		{"project", "import", "--source", "/private/source", "demo"},
		{"project", "import", "retry", "demo"},
		{"project", "export", "--destination", "/private/new-export", "demo"},
	} {
		_, err := parseCommand(append([]string{"--config", "/config", "--domain", "alpha"}, command...), Options{})
		if err != nil {
			t.Fatalf("%v: %v", command, err)
		}
	}
}

type projectStarter struct{ d config.Domain }

func (s projectStarter) Start(_ context.Context, name string) (session.Record, error) {
	return session.LoadRecord(s.d.StateRoot, string(s.d.ID), name)
}

// Keep disk qualification, record publication, clone identity and attachment
// real; replace only VM formatting/start and bounded supervisor transfer.
func projectFixture(t *testing.T) ([]string, config.Domain, Options, *fake.Backend, *bytes.Buffer) {
	t.Helper()
	path, d := writeV2DomainFixture(t, "alpha")
	b := fake.New(backend.Observation{ObjectID: "golden-desktop", Exists: true, State: backend.ObjectStopped})
	out := &bytes.Buffer{}
	o := Options{Output: out, Observer: b, Creator: b, storageCheck: syntheticStorageCheck, SessionStarter: projectStarter{d}, ProjectSetupCheck: func(context.Context, config.Domain, projectx.Setup) error { return nil }}
	prefix := []string{"--config", path, "--domain", "alpha"}
	if err := Run(t.Context(), append(prefix, "golden", "register", "golden-desktop"), o); err != nil {
		t.Fatal(err)
	}
	o.AlphaWorkspaceCreate = func(ctx context.Context, actual config.Domain, _ string, in AlphaWorkspaceCreateInput) (workspacex.Record, error) {
		h, err := workspacex.AcquireStorageOperation(ctx, actual.StateRoot, actual.ID)
		if err != nil {
			return workspacex.Record{}, err
		}
		err = workspacex.SaveRecord(actual.StateRoot, actual.ID, workspacex.Record{Version: 1, Domain: actual.ID, VolumeID: in.VolumeID, FilesystemUUID: in.FilesystemUUID, SizeBytes: in.SizeBytes, Format: workspacex.FormatRawExt4, State: workspacex.StateCreating})
		err = errors.Join(err, h.Release())
		if err != nil {
			return workspacex.Record{}, err
		}
		_, err = workspaceformat.Create(ctx, actual.StateRoot, workspaceformat.Request{Domain: actual.ID, VolumeID: in.VolumeID, FilesystemUUID: in.FilesystemUUID, SizeBytes: in.SizeBytes}, appFormatFunc(func(_ context.Context, r workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
			f, err := os.OpenFile(r.DiskPath, os.O_WRONLY, 0)
			if err != nil {
				return workspaceformat.FormatEvidence{}, err
			}
			defer f.Close()
			raw := make([]byte, 1024)
			raw[0x38], raw[0x39] = 0x53, 0xef
			uuid, _ := hex.DecodeString(strings.ReplaceAll(in.FilesystemUUID, "-", ""))
			copy(raw[0x68:], uuid)
			_, err = f.WriteAt(raw, 1024)
			return workspaceformat.FormatEvidence{ObservedUUID: in.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, err
		}))
		if err != nil {
			return workspacex.Record{}, err
		}
		return workspacex.PromoteVerified(ctx, actual.StateRoot, actual.ID, in.VolumeID)
	}
	if err := Run(t.Context(), append(prefix, "project", "setup", "--source-root", "/source", "--formatter-bundle", "/formatter", "--iso", "/ubuntu.iso", "--go", "/tool/go"), o); err != nil {
		t.Fatal(err)
	}
	return prefix, d, o, b, out
}

func TestNamedProjectsRememberDistinctWorkspaceAndNeverReimportOnOpen(t *testing.T) {
	prefix, d, o, b, out := projectFixture(t)
	for _, name := range []string{"first", "second"} {
		if err := Run(t.Context(), append(prefix, "project", "create", "--size-mib", "16", name), o); err != nil {
			t.Fatal(err)
		}
	}
	first, err := projectx.Load(d.StateRoot, d.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := projectx.Load(d.StateRoot, d.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	if first.VolumeID == second.VolumeID || first.SessionID == second.SessionID || !first.Initialized || !second.Initialized {
		t.Fatalf("lost independent bindings: %+v %+v", first, second)
	}
	transfers := 0
	o.AlphaImport = func(_ context.Context, actual config.Domain, in AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		transfers++
		if in.VolumeID != first.VolumeID || in.SessionName != "first" || in.SourcePath != "/private/source" {
			t.Fatalf("wrong transfer target: %+v", in)
		}
		digest := strings.Repeat("a", 64)
		j := workspacex.ImportJournal{ID: in.TransactionID, Domain: actual.ID, SessionName: "first", SessionID: first.SessionID, BackendObject: first.BackendObject, VolumeID: first.VolumeID, FilesystemUUID: first.FilesystemUUID, MountPath: projectMount, Phase: workspacex.ImportTransferring, SourceDigest: digest, FileCount: 2, TotalBytes: 30}
		return j, supervisor.ImportResult{Digest: digest, FileCount: 2, TotalBytes: 30, RemotePath: projectMount + "/boxwarden-import-" + in.TransactionID}, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "--source", "/private/source", "first"), o); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Run(t.Context(), append(prefix, "project", "open", "first"), o); err != nil {
			t.Fatal(err)
		}
	}
	if transfers != 1 || len(b.CloneCalls()) != 2 {
		t.Fatalf("open duplicated transfer or clone: transfers=%d clones=%d", transfers, len(b.CloneCalls()))
	}
	if err := Run(t.Context(), append(prefix, "project", "create", "first"), o); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("name collision: %v", err)
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "--source", "/private/source", "first"), o); err == nil {
		t.Fatal("reimport accepted")
	}
	if !strings.Contains(out.String(), "next transfer: project export") || !strings.Contains(out.String(), "workspace:") {
		t.Fatalf("missing operator actions: %s", out)
	}
	again, _ := projectx.Load(d.StateRoot, d.ID, "second")
	if again != second {
		t.Fatal("second project changed while using first")
	}
}

func TestProjectMissingSetupAndExistingSessionHaveNoNewResources(t *testing.T) {
	path, d := writeV2DomainFixture(t, "alpha")
	out := &bytes.Buffer{}
	o := Options{Output: out, storageCheck: syntheticStorageCheck}
	err := Run(t.Context(), []string{"--config", path, "--domain", "alpha", "project", "create", "first"}, o)
	if err == nil || !strings.Contains(err.Error(), "setup") {
		t.Fatalf("missing setup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(d.StateRoot, "projects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing setup reserved project: %v", err)
	}
	prefix, d, o, _, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "session", "create", "legacy"), o); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), append(prefix, "project", "create", "legacy"), o); err == nil || !strings.Contains(err.Error(), "name") {
		t.Fatalf("adopted legacy session: %v", err)
	}
	if _, err := projectx.Load(d.StateRoot, d.ID, "legacy"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("wrote legacy project: %v", err)
	}
}

func TestProjectUnknownBaseDoesNotReserveName(t *testing.T) {
	prefix, d, o, b, _ := projectFixture(t)
	err := Run(t.Context(), append(prefix, "project", "create", "--base", "unknown-base", "demo"), o)
	if err == nil {
		t.Fatal("accepted unknown base")
	}
	if _, err := projectx.Load(d.StateRoot, d.ID, "demo"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bad base reserved name: %v", err)
	}
	if len(b.CloneCalls()) != 0 {
		t.Fatal("bad base cloned a VM")
	}
}

func TestProjectRunningExportDoesNotCreateDestination(t *testing.T) {
	prefix, d, o, b, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	r.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
	r.ImportSource = "/private/source"
	r.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		t.Fatal(err)
	}
	b.SetObservation(backend.Observation{ObjectID: r.BackendObject, Exists: true, State: backend.ObjectRunning})
	o.AlphaExport = func(context.Context, config.Domain, AlphaExportInput, backend.Observer) (workspacex.ExportJournal, string, error) {
		t.Fatal("running project reached exporter")
		return workspacex.ExportJournal{}, "", nil
	}
	dest := filepath.Join(t.TempDir(), "new-export")
	err := Run(t.Context(), append(prefix, "project", "export", "--destination", dest, "demo"), o)
	if err == nil || !strings.Contains(err.Error(), "exact stopped sandbox") {
		t.Fatalf("running export accepted or wrong guard: %v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("running export created destination: %v", err)
	}
}

func TestProjectImportRetryReusesSelectionAfterEffectFreeFailure(t *testing.T) {
	prefix, d, o, _, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	var id string
	attempts := 0
	o.AlphaImport = func(_ context.Context, actual config.Domain, in AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		attempts++
		if attempts == 1 {
			id = in.TransactionID
			return workspacex.ImportJournal{}, supervisor.ImportResult{}, errors.New("source missing")
		}
		if in.TransactionID != id || in.SourcePath != "/private/source" || in.Resume {
			t.Fatalf("retry reallocated or retargeted selection: %+v", in)
		}
		digest := strings.Repeat("a", 64)
		j := workspacex.ImportJournal{ID: id, Domain: actual.ID, SessionName: r.Name, SessionID: r.SessionID, BackendObject: r.BackendObject, VolumeID: r.VolumeID, FilesystemUUID: r.FilesystemUUID, MountPath: projectMount, Phase: workspacex.ImportTransferring, SourceDigest: digest, FileCount: 1, TotalBytes: 10}
		return j, supervisor.ImportResult{Digest: digest, FileCount: 1, TotalBytes: 10, RemotePath: projectMount + "/boxwarden-import-" + id}, nil
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "--source", "/private/source", "demo"), o); err == nil {
		t.Fatal("first failure hidden")
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "retry", "demo"), o); err != nil {
		t.Fatal(err)
	}
	saved, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	if !saved.Imported || saved.ImportID != id {
		t.Fatalf("retry lost receipt: %+v", saved)
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "retry", "demo"), o); err == nil {
		t.Fatal("completed import retried")
	}
	if attempts != 2 {
		t.Fatalf("retried completed transfer %d times", attempts)
	}
}

func TestProjectExportRejectsDifferentProjectReceiptAndReportsFiles(t *testing.T) {
	for _, mismatch := range []string{"", "volume", "session", "session-name", "backend", "size", "transaction", "selection", "filesystem", "destination", "aborted"} {
		t.Run(mismatch, func(t *testing.T) {
			prefix, d, o, _, out := projectFixture(t)
			if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
				t.Fatal(err)
			}
			r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
			r.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
			r.ImportSource = "/private/source"
			r.Imported = true
			if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(t.TempDir(), "new-export")
			o.AlphaExport = func(_ context.Context, actual config.Domain, in AlphaExportInput, _ backend.Observer) (workspacex.ExportJournal, string, error) {
				if in.VolumeID != r.VolumeID || len(in.Selected) != 1 || in.Selected[0] != "boxwarden-import-"+r.ImportID || in.DestinationParent != dest || in.SourceRoot != "/source" || in.ISOPath != "/ubuntu.iso" || in.GoBinary != "/tool/go" {
					t.Fatalf("lost remembered inputs: %+v", in)
				}
				j := workspacex.ExportJournal{ID: "00112233-4455-6677-8899-aabbccdd0000", Domain: actual.ID, VolumeID: r.VolumeID, SessionID: r.SessionID, SessionName: r.Name, BackendObject: r.BackendObject, FilesystemUUID: r.FilesystemUUID, SizeBytes: r.SizeBytes, Selected: in.Selected, DestinationParent: dest, Phase: workspacex.ExportPublished}
				switch mismatch {
				case "volume":
					j.VolumeID = "11112233-4455-6677-8899-aabbccddeeff"
				case "session":
					j.SessionID = "11112233-4455-6677-8899-aabbccddeeff"
				case "session-name":
					j.SessionName = "other"
				case "backend":
					j.BackendObject = "other-object"
				case "size":
					j.SizeBytes++
				case "transaction":
					j.ID = "bad-id"
				case "selection":
					j.Selected = []string{"wrong"}
				case "filesystem":
					j.FilesystemUUID = "11112233-4455-6677-8899-aabbccddeeff"
				case "destination":
					j.DestinationParent = dest + "-other"
				case "aborted":
					j.Phase = workspacex.ExportAborted
					return j, "", nil
				}
				return j, filepath.Join(dest, strings.ReplaceAll(j.ID, "-", "")), nil
			}
			err := Run(t.Context(), append(prefix, "project", "export", "--destination", dest, "demo"), o)
			if mismatch != "" {
				if err == nil {
					t.Fatalf("accepted %s mismatch", mismatch)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(dest, "00112233445566778899aabbccdd0000", "boxwarden-import-"+r.ImportID)
			if !strings.Contains(out.String(), "project files: "+want) {
				t.Fatalf("missing returned file location: %s", out)
			}
			if err := Run(t.Context(), append(prefix, "project", "export", "--destination", dest, "demo"), o); err == nil {
				t.Fatal("reused destination")
			}
		})
	}
}

func TestProjectImportRetryResumesCapturedSelectionAndRetainsFailure(t *testing.T) {
	prefix, d, o, _, _ := projectFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "demo"), o); err != nil {
		t.Fatal(err)
	}
	r, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	r.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
	r.ImportSource = "/private/source"
	if err := projectx.Save(d.StateRoot, d.ID, r); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d.StateRoot, "imports", r.ImportID), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("captured guest tree differs")
	calls := 0
	o.AlphaImport = func(_ context.Context, _ config.Domain, in AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		calls++
		if !in.Resume || in.SourcePath != "" || in.TransactionID != r.ImportID {
			t.Fatalf("recaptured or retargeted source: %+v", in)
		}
		return workspacex.ImportJournal{}, supervisor.ImportResult{}, sentinel
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "retry", "demo"), o); !errors.Is(err, sentinel) {
		t.Fatalf("hid refused resume: %v", err)
	}
	after, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	if after != r || calls != 1 {
		t.Fatalf("failed retry changed binding: %+v", after)
	}
	if err := os.WriteFile(filepath.Join(d.StateRoot, "imports", r.ImportID+".json"), []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Run(t.Context(), append(prefix, "project", "import", "retry", "demo"), o); err == nil {
		t.Fatal("corrupt journal accepted")
	}
	if calls != 1 {
		t.Fatal("corrupt journal treated as absent")
	}
}

func TestProjectRejectsMalformedInputsAndNonExportableSize(t *testing.T) {
	for _, command := range [][]string{
		{"project", "create", "--size-mib", "4097", "demo"},
		{"project", "create", "--size-mib", "15", "demo"},
		{"project", "open", "../demo"},
		{"project", "import", "--source", "relative", "demo"},
		{"project", "import", "retry", "--source", "/private/source", "demo"},
		{"project", "export", "--destination", "relative", "demo"},
		{"project", "setup", "--source-root", "/source"},
	} {
		if _, err := parseCommand(append([]string{"--config", "/config", "--domain", "alpha"}, command...), Options{}); err == nil {
			t.Fatalf("accepted %v", command)
		}
	}
}

func TestProjectBackingFailurePrecedesBookmarkOrBackendMutation(t *testing.T) {
	path, selected := writeV2DomainFixture(t, "alpha")
	sentinel := errors.New("synthetic backing unavailable")
	for _, op := range []string{"create", "open", "status", "stop"} {
		err := Run(t.Context(), []string{"--config", path, "--domain", "alpha", "project", op, "demo"}, Options{Output: &bytes.Buffer{}, storageCheck: func(hostidentity.StorageExpectation) error { return sentinel }})
		if err == nil || !strings.Contains(err.Error(), sentinel.Error()) {
			t.Fatalf("%s: %v", op, err)
		}
	}
	if _, err := os.Stat(filepath.Join(selected.StateRoot, "projects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backing failure wrote bookmark directory: %v", err)
	}
}
