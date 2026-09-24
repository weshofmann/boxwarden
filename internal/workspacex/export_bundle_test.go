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
			bundlePath := "/private/tmp/boxwarden-alpha-inspector-export." + hex.EncodeToString(suffix[:])
			if err := os.Mkdir(bundlePath, 0o700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(bundlePath) })
			var requestPath string
			builder := func(ctx context.Context, source, iso, request, goBinary string) (string, error) {
				requestPath = request
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
				t.Fatal("builder was not called with a request file")
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
	path, err := runTrustedInspectorBuilder(context.Background(), source, iso, request, goBinary)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	bundle := PreparedInspectorBundle{Path: path, identity: info}
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
