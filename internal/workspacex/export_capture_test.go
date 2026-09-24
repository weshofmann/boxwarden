package workspacex

import (
	"context"
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

func TestBoundExportCaptureHelperProcess(t *testing.T) {
	if len(os.Args) < 3 || os.Args[2] != "bound-export-helper" {
		return
	}
	_, _ = os.Stdout.Write([]byte("BWEX\x00\x010123456789abcdef"))
	_, _ = fmt.Fprintln(os.Stderr, `BOOT_EVIDENCE {"console_bytes":0,"export_bytes":22,"runtime_network_devices":0,"vm_state":"stopped","inspector_mode":"export"}`)
	os.Exit(0)
}

func TestBoundExportCaptureChecksLockRequestArgsAndPostBootSnapshot(t *testing.T) {
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
			path := filepath.Join(dir, "snapshot.raw")
			content := make([]byte, journal.SizeBytes)
			copy(content, "exact offline snapshot")
			if err := os.WriteFile(path, content, 0o600); err != nil {
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
			digest := sha256.Sum256(content)
			ready := journal
			ready.Phase = ExportSnapshotReady
			ready.Snapshot = &ExportSnapshot{Identity: identity, SHA256: hex.EncodeToString(digest[:])}
			if err := advanceExportJournal(context.Background(), root, journal, ready); err != nil {
				t.Fatal(err)
			}
			prepared, err := PrepareExportInspectorRequest(context.Background(), root, ready.Domain, ready.ID)
			if err != nil {
				t.Fatal(err)
			}
			admitCalled, captureCalled := false, false
			admit := func(_ context.Context, bundle, source string, request []byte) (exportx.InspectorBundle, error) {
				admitCalled = true
				if bundle != "/private/synthetic-bundle" || source != "/private/source" || !reflect.DeepEqual(request, prepared.Request) {
					return exportx.InspectorBundle{}, fmt.Errorf("request differs from admitted journal")
				}
				return exportx.InspectorBundle{Helper: os.Args[0], Kernel: "/private/kernel", Initrd: "/private/initrd"}, nil
			}
			capture := func(ctx context.Context, helper string, args []string, parent string) (exportx.CapturedInspectorStream, error) {
				captureCalled = true
				if helper != os.Args[0] || parent != dir || !reflect.DeepEqual(args, []string{
					"boot-export", "/private/kernel", "/private/initrd", path,
					strings.ReplaceAll(journal.ID, "-", ""),
					fmt.Sprint(identity.Device), fmt.Sprint(identity.Inode), "4096",
				}) {
					return exportx.CapturedInspectorStream{}, fmt.Errorf("boot args differ from admitted snapshot")
				}
				probeContext, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
				if acquired, err := lock.Acquire(probeContext, root, "export-work-"+journal.ID); err == nil {
					_ = acquired.Release()
					return exportx.CapturedInspectorStream{}, fmt.Errorf("capture did not hold transaction lock")
				}
				result, err := exportx.CaptureExportInspector(ctx, os.Args[0], []string{"-test.run=^TestBoundExportCaptureHelperProcess$", "bound-export-helper"}, parent)
				if err != nil {
					return result, err
				}
				if mutate {
					content[0] ^= 0xff
					if err := os.WriteFile(path, content, 0o600); err != nil {
						return result, err
					}
				}
				return result, nil
			}
			result, current, err := captureBoundExportInspector(context.Background(), root, domain.ID("work"), ready.ID,
				"/private/synthetic-bundle", "/private/source", admit, capture)
			if !admitCalled || !captureCalled {
				t.Fatal("admitted journal was not bound to helper capture")
			}
			if mutate {
				if err == nil || result.Stream != nil {
					t.Fatalf("changed snapshot exposed spool: %+v, %v", result, err)
				}
				if _, statErr := os.Lstat(filepath.Join(dir, "stream.bin")); !os.IsNotExist(statErr) {
					t.Fatalf("rejected spool remains: %v", statErr)
				}
				return
			}
			if err != nil || current.ID != journal.ID || result.Stream == nil || result.Evidence.Mode != "export" {
				t.Fatalf("valid capture = %+v journal=%+v err=%v", result, current, err)
			}
			if err := result.Remove(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
