package workspacex

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
	"github.com/weshofmann/boxwarden/internal/lock"
)

func TestBuildInspectorBundleUsesExactJournalRequestAndRechecksSnapshot(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(fmt.Sprintf("mutate=%t", mutate), func(t *testing.T) {
			root := privateRoot(t)
			journal := testExportJournal(t.TempDir())
			journal.SizeBytes = 4096
			if err := createExportJournal(root, journal); err != nil {
				t.Fatal(err)
			}
			dir := filepath.Join(root, "exports", journal.ID)
			if err := os.Mkdir(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			snapshotPath := filepath.Join(dir, "snapshot.raw")
			data := make([]byte, journal.SizeBytes)
			copy(data, "journal source data")
			if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(snapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := diskIdentity(info)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(data)
			ready := journal
			ready.Phase = ExportSnapshotReady
			ready.Snapshot = &ExportSnapshot{Identity: identity, SHA256: hex.EncodeToString(digest[:])}
			if err := advanceExportJournal(context.Background(), root, journal, ready); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("work"), journal.ID)
			if err != nil {
				t.Fatal(err)
			}
			var suffix [3]byte
			if _, err := rand.Read(suffix[:]); err != nil {
				t.Fatal(err)
			}
			stagingRoot := filepath.Join(root, "export-builds")
			if err := os.Mkdir(stagingRoot, 0o700); err != nil {
				t.Fatal(err)
			}
			bundlePath := filepath.Join(stagingRoot, "boxwarden-alpha-inspector-export."+hex.EncodeToString(suffix[:]))
			if err := os.Mkdir(bundlePath, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(bundlePath) })
			var requestPath string
			builder := func(ctx context.Context, source, iso, request, goBinary, staging string) (string, error) {
				requestPath = request
				if staging != stagingRoot || filepath.Dir(filepath.Dir(request)) != stagingRoot {
					return "", fmt.Errorf("builder request staging escaped configured state storage: %s", request)
				}
				if source != "/private/source" || iso != "/private/verified.iso" || goBinary != "/private/bin/go" {
					return "", fmt.Errorf("builder received different inputs")
				}
				requestBytes, err := os.ReadFile(request)
				if err != nil || !bytes.Equal(requestBytes, prepared.Request) {
					return "", fmt.Errorf("builder request differs from journal: %v", err)
				}
				requestInfo, err := os.Lstat(request)
				if err != nil || privateRegular(requestInfo) != nil {
					return "", fmt.Errorf("builder request is not private: %v", err)
				}
				probeContext, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
				if acquired, err := lock.Acquire(probeContext, root, "export-work-"+journal.ID); err == nil {
					_ = acquired.Release()
					return "", fmt.Errorf("builder did not hold transaction lock")
				}
				if mutate {
					data[0] ^= 0xff
					if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
						return "", err
					}
				}
				return bundlePath, nil
			}
			admitter := func(_ context.Context, path, source string, request []byte) (exportx.InspectorBundle, error) {
				if path != bundlePath || source != "/private/source" || !reflect.DeepEqual(request, prepared.Request) {
					return exportx.InspectorBundle{}, fmt.Errorf("admission lost request binding")
				}
				return exportx.InspectorBundle{Helper: filepath.Join(path, "alpha-inspector")}, nil
			}
			got, err := buildAdmittedExportInspectorBundle(context.Background(), root, domain.ID("work"), journal.ID,
				"/private/source", "/private/verified.iso", "/private/bin/go", builder, admitter)
			if requestPath == "" {
				t.Fatalf("builder was not called with a request file: %v", err)
			}
			if _, statErr := os.Lstat(requestPath); !os.IsNotExist(statErr) {
				t.Fatalf("temporary request survived build: %v", statErr)
			}
			if mutate {
				if err == nil || got.Path != "" || !strings.Contains(err.Error(), "snapshot changed") {
					t.Fatalf("changed snapshot admitted bundle %+v: %v", got, err)
				}
				if _, statErr := os.Lstat(bundlePath); !os.IsNotExist(statErr) {
					t.Fatalf("rejected bundle survived: %v", statErr)
				}
				return
			}
			if err != nil || got.Path != bundlePath {
				t.Fatalf("valid bundle path = %+v, %v", got, err)
			}
			if err := got.Remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProductionTrustedInspectorBuilder(t *testing.T) {
	source := os.Getenv("BOXWARDEN_TEST_INSPECTOR_SOURCE")
	iso := os.Getenv("BOXWARDEN_TEST_INSPECTOR_ISO")
	goBinary := os.Getenv("BOXWARDEN_TEST_INSPECTOR_GO")
	request := os.Getenv("BOXWARDEN_TEST_INSPECTOR_REQUEST")
	if source == "" && iso == "" && goBinary == "" && request == "" {
		t.Skip("private production builder inputs not supplied")
	}
	if source == "" || iso == "" || goBinary == "" || request == "" {
		t.Fatal("all four private production builder inputs are required")
	}
	staging := privateRoot(t)
	parentInfo, err := os.Lstat(staging)
	if err != nil {
		t.Fatal(err)
	}
	path, err := runTrustedInspectorBuilder(context.Background(), source, iso, request, goBinary, staging)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	bundle := PreparedInspectorBundle{Path: path, identity: info, parentPath: staging, parentIdentity: parentInfo}
	t.Cleanup(func() {
		if err := bundle.Remove(); err != nil {
			t.Errorf("remove disposable production test bundle: %v", err)
		}
	})
	requestBytes, err := os.ReadFile(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := exportx.AdmitInspectorBundle(context.Background(), path, source, requestBytes); err != nil {
		t.Fatalf("newly built production bundle was rejected: %v", err)
	}
}

func TestInspectorBundlePathSupportsConfiguredStateStorage(t *testing.T) {
	root := filepath.Join(privateRoot(t), "export-builds")
	if err := exactPreparedInspectorBundlePath(filepath.Join(root, "boxwarden-alpha-inspector-export.A12b3C"), root); err != nil {
		t.Fatalf("configured state staging path rejected: %v", err)
	}
}

func TestPreparedInspectorBundleCleanupRejectsReplacedParent(t *testing.T) {
	root := privateRoot(t)
	parent := filepath.Join(root, "export-builds")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "boxwarden-alpha-inspector-export.A12b3C")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Lstat(path)
	parentInfo, _ := os.Lstat(parent)
	bundle := PreparedInspectorBundle{Path: path, identity: info, parentPath: parent, parentIdentity: parentInfo}
	original := filepath.Join(root, "old-staging")
	if err := os.Rename(parent, original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	// Move the original bundle back: its inode still matches, but its parent does not.
	if err := os.Rename(filepath.Join(original, filepath.Base(path)), path); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Remove(); err == nil {
		t.Fatal("replaced staging parent admitted for cleanup")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("cleanup touched retained bundle: %v", err)
	}
}

func TestInspectorBundlePathRejectsOtherParentAndTraversal(t *testing.T) {
	root := filepath.Join(privateRoot(t), "export-builds")
	for _, path := range []string{
		"/private/tmp/boxwarden-alpha-inspector-export.A12b3C",
		root + "/../boxwarden-alpha-inspector-export.A12b3C",
		root + "/boxwarden-alpha-inspector-export.A12b3C/child",
		root + "/boxwarden-alpha-inspector-export.A12b3!",
	} {
		if err := exactPreparedInspectorBundlePath(path, root); err == nil {
			t.Fatalf("unsafe path accepted: %s", path)
		}
	}
}

func TestTrustedInspectorBuilderPreservesStagingArgvAndClosedEnvironment(t *testing.T) {
	root := privateRoot(t)
	source := filepath.Join(root, "source with spaces")
	scriptDir := filepath.Join(source, "tools", "alpha-inspector")
	if err := os.MkdirAll(scriptDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(root, "staging with spaces")
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin with spaces")
	if err := os.Mkdir(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	goBinary := filepath.Join(binDir, "go")
	if err := os.WriteFile(goBinary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	iso := filepath.Join(root, "input with spaces.iso")
	request := filepath.Join(root, "request with spaces.json")
	script := `#!/bin/bash
set -eu
[[ "$#" == 3 ]]
[[ "${TMPDIR:-}" == "$3" ]]
[[ -z "${BOXWARDEN_TEST_AMBIENT_SENTINEL:-}" ]]
printf '%s\n' "$1" "$2" "$3" "${TMPDIR:-}" "${BOXWARDEN_TEST_AMBIENT_SENTINEL:-}" > "$3/argv.txt"
printf 'prepared private export inspector artifacts: %s/boxwarden-alpha-inspector-export.A12b3C\n' "$3"
`
	if err := os.WriteFile(filepath.Join(scriptDir, "prepare_export_bundle.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOXWARDEN_TEST_AMBIENT_SENTINEL", "must-not-reach-child")
	got, err := runTrustedInspectorBuilder(context.Background(), source, iso, request, goBinary, staging)
	if err != nil || got != filepath.Join(staging, "boxwarden-alpha-inspector-export.A12b3C") {
		t.Fatalf("builder path=%q err=%v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(staging, "argv.txt"))
	if err != nil || string(raw) != iso+"\n"+request+"\n"+staging+"\n"+staging+"\n\n" {
		t.Fatalf("child argv changed: %q, %v", raw, err)
	}
}
