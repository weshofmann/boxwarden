package workspacex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
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
			stagingRoot := filepath.Join(root, "export-builds")
			var bundlePath string
			var requestPath string
			builder := func(ctx context.Context, source, iso, request, goBinary, output string) (string, error) {
				requestPath = request
				bundlePath = output
				if filepath.Dir(output) != stagingRoot || filepath.Dir(filepath.Dir(request)) != stagingRoot {
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
	output := filepath.Join(staging, "boxwarden-alpha-inspector-export.A12b3C")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := runTrustedInspectorBuilder(context.Background(), source, iso, request, goBinary, output)
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
printf '%s\n' "$#" "$1" "$2" "$3" "${TMPDIR:-}" "${BOXWARDEN_TEST_AMBIENT_SENTINEL:-}" > "$3/argv.txt"
printf 'prepared private export inspector artifacts: %s\n' "$3"
`
	if err := os.WriteFile(filepath.Join(scriptDir, "prepare_export_bundle.sh"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BOXWARDEN_TEST_AMBIENT_SENTINEL", "must-not-reach-child")
	output := filepath.Join(staging, "boxwarden-alpha-inspector-export.A12b3C")
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	got, err := runTrustedInspectorBuilder(context.Background(), source, iso, request, goBinary, output)
	if err != nil || got != filepath.Join(staging, "boxwarden-alpha-inspector-export.A12b3C") {
		t.Fatalf("builder path=%q err=%v", got, err)
	}
	raw, err := os.ReadFile(filepath.Join(output, "argv.txt"))
	if err != nil || string(raw) != "3\n"+iso+"\n"+request+"\n"+output+"\n"+output+"\n\n" {
		t.Fatalf("child argv changed: %q, %v", raw, err)
	}
}

func TestExportBuilderDescendantHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-3] != "--builder-descendant" {
		return
	}
	conn, err := net.Dial("unix", os.Args[len(os.Args)-2])
	if err != nil {
		os.Exit(31)
	}
	_, _ = conn.Write([]byte("ready"))
	var release [1]byte
	if _, err := conn.Read(release[:]); err == nil {
		_ = os.WriteFile(os.Args[len(os.Args)-1], []byte("late"), 0600)
	}
	_ = conn.Close()
	os.Exit(0)
}

func TestTrustedInspectorBuilderCancellationTerminatesDescendants(t *testing.T) {
	realBuilderLifetimeFixture(t, false)
}
func TestTrustedInspectorBuilderUnprovenDrainRetainsArtifacts(t *testing.T) {
	realBuilderLifetimeFixture(t, true)
}
func realBuilderLifetimeFixture(t *testing.T, unproven bool) {
	root := privateRoot(t)
	journal := snapshotReadyBuilderJournal(t, root)
	staging := filepath.Join(root, "export-builds")
	if err := os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(staging, "neighbor")
	if err := os.WriteFile(neighbor, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	// Darwin Unix socket paths are short; the fixture owns this disposable socket only.
	socketDir, err := os.MkdirTemp("/tmp", "bw-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(socketDir)
	socket := filepath.Join(socketDir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	source := filepath.Join(root, "source")
	scripts := filepath.Join(source, "tools", "alpha-inspector")
	if err := os.MkdirAll(scripts, 0700); err != nil {
		t.Fatal(err)
	}
	binary, _ := os.Executable()
	sideEffect := filepath.Join(root, "late")
	finish := "wait\n"
	if unproven {
		finish = "exit 0\n"
	}
	script := "#!/bin/bash\nprintf partial > \"$3/partial\"\n" + shellBuilderFixtureQuote(binary) + " -test.run=^TestExportBuilderDescendantHelper$ -- --builder-descendant " + shellBuilderFixtureQuote(socket) + " " + shellBuilderFixtureQuote(sideEffect) + " &\n" + finish
	if err := os.WriteFile(filepath.Join(scripts, "prepare_export_bundle.sh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	goBinary := filepath.Join(root, "go")
	if err := os.WriteFile(goBinary, []byte("#!/bin/sh\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	paths := make(chan [2]string, 1)
	builder := func(ctx context.Context, source, iso, request, goBinary, output string) (string, error) {
		paths <- [2]string{filepath.Dir(request), output}
		return runTrustedInspectorBuilder(ctx, source, iso, request, goBinary, output)
	}
	admitter := func(context.Context, string, string, []byte) (exportx.InspectorBundle, error) {
		t.Error("cancelled builder admitted")
		return exportx.InspectorBundle{}, nil
	}
	go func() {
		_, err := buildAdmittedExportInspectorBundle(ctx, root, domain.ID("work"), journal.ID, source, "/private/input.iso", goBinary, builder, admitter)
		result <- err
	}()
	listener.(*net.UnixListener).SetDeadline(time.Now().Add(5 * time.Second))
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	ready := make([]byte, 5)
	if _, err := io.ReadFull(conn, ready); err != nil || string(ready) != "ready" {
		t.Fatalf("ready=%q: %v", ready, err)
	}
	ownedPaths := <-paths
	if !unproven {
		cancel()
	}
	select {
	case err := <-result:
		if err == nil {
			t.Error("unresolved builder succeeded")
		}
		if unproven && !errors.Is(err, errExportBuilderLifetimeUnproven) {
			t.Errorf("drain not classified unproven: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("builder cancellation did not return within bound")
	}
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var data [1]byte
	_, err = conn.Read(data[:])
	if unproven {
		if err == io.EOF {
			t.Error("fixture writer unexpectedly terminated")
		}
		// Release the deliberate contract-violating writer only after retention is established.
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("release"))
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Read(data[:]); err != io.EOF {
			t.Errorf("released fixture did not close socket: %v", err)
		}
	} else if err != io.EOF {
		t.Errorf("descendant survived cancellation: socket read=%v", err)
		conn.SetWriteDeadline(time.Now().Add(time.Second))
		_, _ = conn.Write([]byte("release"))
	}
	if _, err := os.Lstat(sideEffect); !unproven && !os.IsNotExist(err) {
		t.Errorf("late side effect: %v", err)
	}
	for _, path := range ownedPaths {
		_, err := os.Lstat(path)
		if unproven && err != nil {
			t.Errorf("unproven artifact removed: %s %v", path, err)
		}
		if !unproven && !os.IsNotExist(err) {
			t.Errorf("cancelled artifact retained: %s %v", path, err)
		}
	}
	if raw, err := os.ReadFile(neighbor); err != nil || string(raw) != "untouched" {
		t.Errorf("neighbor changed: %q %v", raw, err)
	}
}

func snapshotReadyBuilderJournal(t *testing.T, root string) ExportJournal {
	t.Helper()
	journal := testExportJournal(t.TempDir())
	journal.SizeBytes = 4096
	if err := createExportJournal(root, journal); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "exports", journal.ID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "snapshot.raw")
	data := make([]byte, journal.SizeBytes)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
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
	return ready
}

func TestInspectorBuilderCleanupRequiresProvenLifetime(t *testing.T) {
	for _, unproven := range []bool{false, true} {
		t.Run(fmt.Sprintf("unproven=%t", unproven), func(t *testing.T) {
			root := privateRoot(t)
			journal := snapshotReadyBuilderJournal(t, root)
			staging := filepath.Join(root, "export-builds")
			if err := os.Mkdir(staging, 0700); err != nil {
				t.Fatal(err)
			}
			neighbor := filepath.Join(staging, "neighbor")
			if err := os.WriteFile(neighbor, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			var requestPath, outputPath string
			builder := func(_ context.Context, _, _, request, _, output string) (string, error) {
				requestPath, outputPath = request, output
				entries, err := os.ReadDir(output)
				if err != nil || len(entries) != 0 {
					t.Fatalf("output not preallocated empty: %v", err)
				}
				if err := os.WriteFile(filepath.Join(output, "partial"), []byte("partial"), 0600); err != nil {
					t.Fatal(err)
				}
				if unproven {
					return "", errExportBuilderLifetimeUnproven
				}
				return "", context.Canceled
			}
			admitter := func(context.Context, string, string, []byte) (exportx.InspectorBundle, error) {
				t.Fatal("failed builder admitted")
				return exportx.InspectorBundle{}, nil
			}
			bundle, err := buildAdmittedExportInspectorBundle(context.Background(), root, domain.ID("work"), journal.ID, "/private/source", "/private/input.iso", "/private/bin/go", builder, admitter)
			if err == nil || bundle.Path != "" {
				t.Fatalf("failed build returned cleanup authority: %+v %v", bundle, err)
			}
			if unproven && (!errors.Is(err, errExportBuilderLifetimeUnproven) || !strings.Contains(err.Error(), requestPath[:strings.LastIndex(requestPath, "/")]) || !strings.Contains(err.Error(), outputPath)) {
				t.Fatalf("missing retained receipt: %v", err)
			}
			for _, path := range []string{filepath.Dir(requestPath), outputPath} {
				_, statErr := os.Lstat(path)
				if unproven && statErr != nil {
					t.Fatalf("unproven builder artifact removed: %s %v", path, statErr)
				}
				if !unproven && !os.IsNotExist(statErr) {
					t.Fatalf("resolved builder artifact retained: %s %v", path, statErr)
				}
			}
			raw, err := os.ReadFile(neighbor)
			if err != nil || string(raw) != "untouched" {
				t.Fatalf("neighbor altered: %q %v", raw, err)
			}
		})
	}
}

func shellBuilderFixtureQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
