package closeout

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"testing"
)

func archiveFixture(t *testing.T) (string, contract.Handoff) {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	w := contract.Window{LockSHA: contract.SHA([]byte("lock")), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 1, ExpiresUnixNS: 1 + contract.WindowNS, ContinuousStartNS: 2, ContinuousLimitNS: 2 + contract.WindowNS}
	b := []byte("{\"version\":1}")
	m := contract.ArchiveManifest{Version: 1, Window: w, DispatchClosed: true, RuntimeClean: true, Entries: []contract.ArchiveEntry{{Name: "receipt-archive.json", SHA: contract.SHA(b), Bytes: uint32(len(b))}}}
	raw, e := contract.EncodeArchive(m)
	if e != nil {
		t.Fatal(e)
	}
	for n, data := range map[string][]byte{"receipt-archive.json": b, contract.ArchiveName: raw} {
		if e = os.WriteFile(filepath.Join(root, n), data, 0600); e != nil {
			t.Fatal(e)
		}
	}
	return root, contract.Handoff{Window: w, ArchiveSHA: contract.SHA(raw), ArchiveBytes: uint64(len(raw) + len(b)), ArchiveFiles: 2}
}
func TestArchiveExactBytesAndExhaustiveClosedMembership(t *testing.T) {
	for _, mode := range []string{"positive", "extra", "changed", "link", "mode", "window", "count", "bytes", "lost-index", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			r, h := archiveFixture(t)
			switch mode {
			case "extra":
				os.WriteFile(filepath.Join(r, "foreign.json"), []byte("{}"), 0600)
			case "changed":
				os.WriteFile(filepath.Join(r, "receipt-archive.json"), []byte("{\"changed\":true}"), 0600)
			case "link":
				os.Link(filepath.Join(r, "receipt-archive.json"), filepath.Join(r, "second-link"))
			case "mode":
				os.Chmod(filepath.Join(r, "receipt-archive.json"), 0644)
			case "window":
				h.Window.ID = "22222222-2222-2222-2222-222222222222"
			case "count":
				h.ArchiveFiles++
			case "bytes":
				h.ArchiveBytes++
			case "lost-index":
				os.Remove(filepath.Join(r, contract.ArchiveName))
			}
			ctx := t.Context()
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(ctx)
				cancel()
				if verifyArchive(ctx, r, os.Getuid(), noACL{}, h) == nil {
					t.Fatal("cancelled archive admitted")
				}
				return
			}
			e := verifyArchive(ctx, r, os.Getuid(), noACL{}, h)
			if mode == "positive" {
				if e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("uncertain archive admitted")
			}
		})
	}
}
