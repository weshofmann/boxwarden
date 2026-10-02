package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// These public-command tests retain real project/journal admission. Only the
// expensive admitted VM inspector is substituted at its existing driver seam.
func TestProjectExportRetryReusesExactTransactionAndSavedAssets(t *testing.T) {
	for _, phase := range []workspacex.ExportPhase{workspacex.ExportSnapshotReady, workspacex.ExportCopying} {
		t.Run(string(phase), func(t *testing.T) {
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
			j := projectRetryJournal(r, d, filepath.Join(t.TempDir(), "retained-destination"), phase)
			saveProjectRetryJournal(t, d, j)
			before := snapshotProjectTree(t, d.StateRoot)
			o.AlphaExport = func(context.Context, config.Domain, AlphaExportInput, backend.Observer) (workspacex.ExportJournal, string, error) {
				t.Fatal("retry started a new export")
				return j, "", nil
			}
			o.AlphaExportResume = func(_ context.Context, actual config.Domain, in AlphaExportResumeInput, _ backend.Observer) (workspacex.ExportJournal, string, error) {
				if actual != d || in.TransactionID != j.ID || in.SourceRoot != "/source" || in.ISOPath != "/ubuntu.iso" || in.GoBinary != "/tool/go" {
					t.Fatalf("lost exact transaction/assets: %+v", in)
				}
				next := j
				if phase == workspacex.ExportCopying {
					next.Phase = workspacex.ExportAborted
					return next, "", nil
				}
				next.Phase = workspacex.ExportPublished
				return next, filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", "")), nil
			}
			out.Reset()
			if err := Run(t.Context(), append(prefix, "project", "export", "retry", "--transaction", j.ID, "demo"), o); err != nil {
				t.Fatal(err)
			}
			if phase == workspacex.ExportCopying {
				if !strings.Contains(out.String(), "export: aborted") || !strings.Contains(out.String(), "new destination") || strings.Contains(out.String(), "project files:") {
					t.Fatalf("ambiguous aborted result: %s", out)
				}
			} else if !strings.Contains(out.String(), "project files: "+filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", ""), "boxwarden-import-"+r.ImportID)) {
				t.Fatalf("missing returned location: %s", out)
			}
			// The wrapper does not allocate a destination or rewrite source/journals.
			after := snapshotProjectTree(t, d.StateRoot)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("retry allocated unrelated state")
			}
			if _, err := os.Stat(j.DestinationParent); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("wrapper recreated destination: %v", err)
			}
		})
	}
}

func projectRetryJournal(r projectx.Record, d config.Domain, dest string, phase workspacex.ExportPhase) workspacex.ExportJournal {
	id := "00112233-4455-6677-8899-aabbccdd0000"
	j := workspacex.ExportJournal{Version: 1, ID: id, Domain: d.ID, VolumeID: r.VolumeID, SessionID: r.SessionID, SessionName: r.Name, BackendObject: r.BackendObject, FilesystemUUID: r.FilesystemUUID, SizeBytes: r.SizeBytes, Source: workspacex.DiskIdentity{Device: 1, Inode: 2}, SnapshotPath: filepath.Join("exports", id, "snapshot.raw"), DestinationParent: dest, Destination: workspacex.DiskIdentity{Device: 3, Inode: 4}, Selected: []string{"boxwarden-import-" + r.ImportID}, Phase: phase}
	if phase != workspacex.ExportCopying {
		j.Snapshot = &workspacex.ExportSnapshot{Identity: workspacex.DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
	}
	return j
}
func saveProjectRetryJournal(t *testing.T, d config.Domain, j workspacex.ExportJournal) {
	t.Helper()
	dir := filepath.Join(d.StateRoot, "exports")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, j.ID+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectExportRetryRejectsForeignJournalBeforeDriver(t *testing.T) {
	for _, mismatch := range []string{"volume", "session", "backend", "filesystem", "size", "selection", "domain", "symlink", "published"} {
		t.Run(mismatch, func(t *testing.T) {
			prefix, d, o, _, _ := projectFixture(t)
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
			j := projectRetryJournal(r, d, "/private/retained", workspacex.ExportSnapshotReady)
			switch mismatch {
			case "volume":
				j.VolumeID = "11112233-4455-6677-8899-aabbccddeeff"
			case "session":
				j.SessionID = "11112233-4455-6677-8899-aabbccddeeff"
			case "backend":
				j.BackendObject = "other-system"
			case "filesystem":
				j.FilesystemUUID = "11112233-4455-6677-8899-aabbccddeeff"
			case "size":
				j.SizeBytes += 512
			case "selection":
				j.Selected = []string{"other-project"}
			case "domain":
				j.Domain = "other"
			case "published":
				j.Phase = workspacex.ExportPublished
			}
			saveProjectRetryJournal(t, d, j)
			if mismatch == "symlink" {
				path := filepath.Join(d.StateRoot, "exports", j.ID+".json")
				if err := os.Rename(path, path+"-held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path+"-held", path); err != nil {
					t.Fatal(err)
				}
			}
			o.AlphaExportResume = func(context.Context, config.Domain, AlphaExportResumeInput, backend.Observer) (workspacex.ExportJournal, string, error) {
				t.Fatal("foreign or published journal reached recovery")
				return j, "", nil
			}
			if err := Run(t.Context(), append(prefix, "project", "export", "retry", "--transaction", j.ID, "demo"), o); err == nil {
				t.Fatal("unsafe retry accepted")
			}
		})
	}
}

func TestProjectExportRetryRejectsChangedRecoveryReceipt(t *testing.T) {
	for _, mismatch := range []string{"destination", "transaction", "snapshot", "phase", "published-path"} {
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
			j := projectRetryJournal(r, d, "/private/retained", workspacex.ExportSnapshotReady)
			saveProjectRetryJournal(t, d, j)
			o.AlphaExportResume = func(context.Context, config.Domain, AlphaExportResumeInput, backend.Observer) (workspacex.ExportJournal, string, error) {
				next := j
				next.Phase = workspacex.ExportPublished
				published := filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", ""))
				switch mismatch {
				case "destination":
					next.DestinationParent = "/private/other"
				case "transaction":
					next.ID = "11112233-4455-6677-8899-aabbccdd0000"
				case "snapshot":
					copy := *j.Snapshot
					copy.SHA256 = strings.Repeat("b", 64)
					next.Snapshot = &copy
				case "phase":
					next.Phase = workspacex.ExportAborted
				case "published-path":
					published = "/private/other"
				}
				return next, published, nil
			}
			out.Reset()
			if err := Run(t.Context(), append(prefix, "project", "export", "retry", "--transaction", j.ID, "demo"), o); err == nil {
				t.Fatal("changed recovery receipt accepted")
			}
			if strings.Contains(out.String(), "project files:") {
				t.Fatalf("claimed returned project after wrong receipt: %s", out)
			}
		})
	}
}
