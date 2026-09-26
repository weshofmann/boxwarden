package workspacex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
)

func exportPublishFrame(kind byte, name string, chunk []byte, declared uint64, digest [32]byte) []byte {
	var output bytes.Buffer
	output.WriteByte(kind)
	_ = binary.Write(&output, binary.BigEndian, uint16(len(name)))
	_ = binary.Write(&output, binary.BigEndian, uint32(len(chunk)))
	_ = binary.Write(&output, binary.BigEndian, declared)
	output.Write(digest[:])
	output.WriteString(name)
	output.Write(chunk)
	return output.Bytes()
}

func TestExportPublishHelperProcess(t *testing.T) {
	if len(os.Args) < 5 || os.Args[2] != "publish-helper" {
		return
	}
	transaction, err := hex.DecodeString(os.Args[3])
	if err != nil || len(transaction) != 16 {
		os.Exit(7)
	}
	var stream bytes.Buffer
	stream.Write([]byte{'B', 'W', 'E', 'X', 0, 1})
	stream.Write(transaction)
	stream.Write(exportPublishFrame(1, "project", nil, 0, [32]byte{}))
	stream.Write(exportPublishFrame(2, "project/report.txt", nil, 5, [32]byte{}))
	stream.Write(exportPublishFrame(3, "", []byte("hello"), 0, [32]byte{}))
	stream.Write(exportPublishFrame(4, "", nil, 0, sha256.Sum256([]byte("hello"))))
	if os.Args[4] == "extra" {
		stream.Write(exportPublishFrame(2, "project/secret.txt", nil, 0, [32]byte{}))
		stream.Write(exportPublishFrame(4, "", nil, 0, sha256.Sum256(nil)))
	}
	stream.Write(exportPublishFrame(5, "", nil, 5, [32]byte{}))
	_, _ = os.Stdout.Write(stream.Bytes())
	_, _ = fmt.Fprintf(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":0,\"export_bytes\":%d,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n", stream.Len())
	os.Exit(0)
}

func TestPublishCapturedExportSelectedTreeAndInspectedFailure(t *testing.T) {
	for _, scenario := range []struct {
		name, mode string
		mutate     bool
		wantPhase  ExportPhase
	}{
		{"selected tree", "valid", false, ExportPublished},
		{"extra path", "extra", false, ExportInspected},
		{"snapshot changed", "valid", true, ExportSnapshotReady},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := privateRoot(t)
			destination := filepath.Join(t.TempDir(), "returned")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			parentInfo, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			parentID, err := diskIdentity(parentInfo)
			if err != nil {
				t.Fatal(err)
			}
			journal := testExportJournal(destination)
			journal.SizeBytes = 4096
			journal.Destination = parentID
			if err := createExportJournal(root, journal); err != nil {
				t.Fatal(err)
			}
			snapshotDir := filepath.Join(root, "exports", journal.ID)
			if err := os.Mkdir(snapshotDir, 0o700); err != nil {
				t.Fatal(err)
			}
			snapshotPath := filepath.Join(snapshotDir, "snapshot.raw")
			content := make([]byte, journal.SizeBytes)
			copy(content, "qualified snapshot")
			if err := os.WriteFile(snapshotPath, content, 0o600); err != nil {
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
			digest := sha256.Sum256(content)
			ready := journal
			ready.Phase = ExportSnapshotReady
			ready.Snapshot = &ExportSnapshot{Identity: identity, SHA256: hex.EncodeToString(digest[:])}
			if err := advanceExportJournal(context.Background(), root, journal, ready); err != nil {
				t.Fatal(err)
			}
			captured, err := exportx.CaptureExportInspector(context.Background(), os.Args[0], []string{
				"-test.run=^TestExportPublishHelperProcess$", "publish-helper", exportTransactionHex(journal.ID), scenario.mode,
			}, snapshotDir)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.mutate {
				content[0] ^= 0xff
				if err := os.WriteFile(snapshotPath, content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path, err := publishCapturedExport(context.Background(), root, domain.ID("work"), journal.ID, captured, allowSyntheticExportHeadroom, syntheticExportReceiverReserve)
			stored, loadErr := loadExportJournal(root, domain.ID("work"), journal.ID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if _, statErr := os.Lstat(filepath.Join(snapshotDir, "stream.bin")); !os.IsNotExist(statErr) {
				t.Fatalf("publication left private spool: %v", statErr)
			}
			if scenario.wantPhase != ExportPublished {
				if err == nil || path != "" || stored.Phase != scenario.wantPhase {
					t.Fatalf("extra unselected path published: %q %+v %v", path, stored.Phase, err)
				}
				entries, err := os.ReadDir(destination)
				if err != nil || len(entries) != 0 {
					t.Fatalf("rejected destination changed: %v %v", entries, err)
				}
				if scenario.mode == "extra" {
					if _, err := PrepareExportInspectorRequest(context.Background(), root, domain.ID("work"), journal.ID); err != nil {
						t.Fatalf("inspected retry could not derive request: %v", err)
					}
					retry, err := exportx.CaptureExportInspector(context.Background(), os.Args[0], []string{
						"-test.run=^TestExportPublishHelperProcess$", "publish-helper", exportTransactionHex(journal.ID), "valid",
					}, snapshotDir)
					if err != nil {
						t.Fatal(err)
					}
					path, err := publishCapturedExport(context.Background(), root, domain.ID("work"), journal.ID, retry, allowSyntheticExportHeadroom, syntheticExportReceiverReserve)
					if err != nil || path != filepath.Join(destination, exportTransactionHex(journal.ID)) {
						t.Fatalf("inspected retry publication = %q, %v", path, err)
					}
					stored, err = loadExportJournal(root, domain.ID("work"), journal.ID)
					if err != nil || stored.Phase != ExportPublished {
						t.Fatalf("retry did not durably publish: %s, %v", stored.Phase, err)
					}
				}
				return
			}
			if err != nil || stored.Phase != ExportPublished || path != filepath.Join(destination, exportTransactionHex(journal.ID)) {
				t.Fatalf("publication = %q phase=%s err=%v", path, stored.Phase, err)
			}
			returned, err := os.ReadFile(filepath.Join(path, "project", "report.txt"))
			if err != nil || string(returned) != "hello" {
				t.Fatalf("selected return = %q %v", returned, err)
			}
			if strings.Contains(path, "stream.bin") {
				t.Fatal("private spool path returned as publication")
			}
		})
	}
}
