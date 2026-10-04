package workspacex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/exportx"
)

func TestExportJournalAndRequestAdmitFourGiBDisk(t *testing.T) {
	for _, size := range []int64{(1 << 30) + 512, 4 << 30, (4 << 30) + 512} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			root := privateRoot(t)
			journal := testExportJournal(t.TempDir())
			journal.SizeBytes = size
			err := createExportJournal(root, journal)
			if size > 4<<30 {
				if err == nil {
					t.Fatal("raw disk above 4 GiB admitted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := loadExportJournal(root, domain.ID("work"), journal.ID)
			if err != nil || loaded.SizeBytes != size {
				t.Fatalf("journal disk binding = %d, %v", loaded.SizeBytes, err)
			}
			loaded.Phase = ExportSnapshotReady
			loaded.Snapshot = &ExportSnapshot{Identity: DiskIdentity{Device: 3, Inode: 5}, SHA256: strings.Repeat("a", 64)}
			raw, err := encodeExportInspectorRequest(loaded)
			if err != nil {
				t.Fatal(err)
			}
			var request struct {
				DiskBytes int64 `json:"disk_bytes"`
			}
			if err := json.Unmarshal(raw, &request); err != nil || request.DiskBytes != size {
				t.Fatalf("request disk binding = %d, %v", request.DiskBytes, err)
			}
		})
	}
}

func TestLargerExportHelperProcess(t *testing.T) {
	if len(os.Args) < 5 || os.Args[2] != "larger-export-helper" {
		return
	}
	transaction, err := hex.DecodeString(os.Args[3])
	if err != nil || len(transaction) != 16 {
		os.Exit(7)
	}
	var written int64
	emit := func(data []byte) {
		n, err := os.Stdout.Write(data)
		if err != nil || n != len(data) {
			os.Exit(8)
		}
		written += int64(n)
	}
	emit(append([]byte{'B', 'W', 'E', 'X', 0, 1}, transaction...))
	emit(exportPublishFrame(1, "project", nil, 0, [32]byte{}))
	var total uint64
	if strings.HasPrefix(os.Args[4], "files-") {
		count := 8192
		if os.Args[4] == "files-over" {
			count++
		}
		for i := 0; i < count; i++ {
			emit(exportPublishFrame(2, fmt.Sprintf("project/f%04d", i), nil, 0, [32]byte{}))
			emit(exportPublishFrame(4, "", nil, 0, sha256.Sum256(nil)))
		}
	} else {
		sizes := []int64{128 << 20, (128 << 20) + 1}
		if os.Args[4] == "total-over" {
			sizes = []int64{256 << 20, 256 << 20}
		}
		chunk := make([]byte, 1<<20)
		for i, size := range sizes {
			emit(exportPublishFrame(2, fmt.Sprintf("project/f%d", i), nil, uint64(size), [32]byte{}))
			digest := sha256.New()
			for remaining := size; remaining > 0; {
				data := chunk[:min(int64(len(chunk)), remaining)]
				emit(exportPublishFrame(3, "", data, 0, [32]byte{}))
				_, _ = digest.Write(data)
				remaining -= int64(len(data))
			}
			var sum [32]byte
			copy(sum[:], digest.Sum(nil))
			emit(exportPublishFrame(4, "", nil, 0, sum))
			total += uint64(size)
		}
		if os.Args[4] == "total-over" {
			emit(exportPublishFrame(2, "project/too-much", nil, 1, [32]byte{}))
		}
	}
	emit(exportPublishFrame(5, "", nil, total, [32]byte{}))
	_, _ = fmt.Fprintf(os.Stderr, "BOOT_EVIDENCE {\"console_bytes\":0,\"export_bytes\":%d,\"runtime_network_devices\":0,\"vm_state\":\"stopped\",\"inspector_mode\":\"export\"}\n", written)
	os.Exit(0)
}

func TestProductionExportPublicationLargerSelectionBounds(t *testing.T) {
	for _, tc := range []struct {
		mode string
		pass bool
	}{{"files-bound", true}, {"files-over", false}, {"total-above-old", true}, {"total-over", false}} {
		t.Run(tc.mode, func(t *testing.T) {
			root, destination := privateRoot(t), t.TempDir()
			if err := os.Chmod(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			parentInfo, err := os.Stat(destination)
			if err != nil {
				t.Fatal(err)
			}
			parentID, err := diskIdentity(parentInfo)
			if err != nil {
				t.Fatal(err)
			}
			journal := testExportJournal(destination)
			journal.SizeBytes, journal.Destination, journal.Selected = 4096, parentID, []string{"project"}
			if err := createExportJournal(root, journal); err != nil {
				t.Fatal(err)
			}
			snapshotDir := filepath.Join(root, "exports", journal.ID)
			if err := os.Mkdir(snapshotDir, 0o700); err != nil {
				t.Fatal(err)
			}
			data := make([]byte, 4096)
			snapshotPath := filepath.Join(snapshotDir, "snapshot.raw")
			if err := os.WriteFile(snapshotPath, data, 0o600); err != nil {
				t.Fatal(err)
			}
			info, err := os.Stat(snapshotPath)
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
			captured, err := exportx.CaptureExportInspector(context.Background(), os.Args[0], []string{"-test.run=^TestLargerExportHelperProcess$", "larger-export-helper", exportTransactionHex(journal.ID), tc.mode}, snapshotDir)
			if err != nil {
				t.Fatal(err)
			}
			path, publishErr := publishCapturedExport(context.Background(), root, domain.ID("work"), journal.ID, captured, allowSyntheticExportHeadroom, syntheticExportReceiverReserve)
			stored, err := loadExportJournal(root, domain.ID("work"), journal.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.pass {
				if publishErr == nil || !strings.Contains(publishErr.Error(), "file size or count exceeds limit") || path != "" || stored.Phase != ExportInspected {
					t.Fatalf("over-limit publication = %q, phase=%s, err=%v", path, stored.Phase, publishErr)
				}
				children, err := os.ReadDir(destination)
				if err != nil || len(children) != 0 {
					t.Fatalf("failed publication changed destination: %v, %v", children, err)
				}
			} else {
				if publishErr != nil || stored.Phase != ExportPublished {
					t.Fatalf("bounded selection rejected: path=%q, phase=%s, err=%v", path, stored.Phase, publishErr)
				}
				if tc.mode == "files-bound" {
					children, err := os.ReadDir(filepath.Join(path, "project"))
					if err != nil || len(children) != 8192 {
						t.Fatalf("published count=%d, err=%v", len(children), err)
					}
				} else {
					var total int64
					for _, name := range []string{"f0", "f1"} {
						file, err := os.Open(filepath.Join(path, "project", name))
						if err != nil {
							t.Fatal(err)
						}
						n, err := io.Copy(io.Discard, file)
						_ = file.Close()
						if err != nil {
							t.Fatal(err)
						}
						total += n
					}
					if total != (256<<20)+1 {
						t.Fatalf("published total=%d", total)
					}
				}
			}
			if _, err := os.Stat(filepath.Join(snapshotDir, "stream.bin")); !os.IsNotExist(err) {
				t.Fatalf("captured spool survived publication: %v", err)
			}
		})
	}
}
