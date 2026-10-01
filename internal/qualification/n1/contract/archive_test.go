package contract

import (
	"strings"
	"testing"
)

func TestArchiveManifestBoundsCanonicalAndFixedNames(t *testing.T) {
	m := ArchiveManifest{Version: 1, Window: Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 1, ExpiresUnixNS: 1 + WindowNS, ContinuousStartNS: 2, ContinuousLimitNS: 2 + WindowNS}, DispatchClosed: true, RuntimeClean: true, Entries: []ArchiveEntry{{Name: "receipt-archive.json", SHA: strings.Repeat("b", 64), Bytes: 100}}}
	raw, e := EncodeArchive(m)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := ParseArchive(raw); e != nil || got.Window != m.Window {
		t.Fatal(e)
	}
	for _, which := range []string{"path", "raw", "credential", "unknown", "duplicate", "bytes", "window", "open", "dirty", "empty", "order"} {
		t.Run(which, func(t *testing.T) {
			n := m
			n.Entries = append([]ArchiveEntry(nil), m.Entries...)
			switch which {
			case "path":
				n.Entries[0].Name = "../receipt-archive.json"
			case "raw":
				n.Entries[0].Name = "stdout.log"
			case "credential":
				n.Entries[0].Name = "ca"
			case "unknown":
				n.Entries[0].Name = "receipt-arbitrary.json"
			case "duplicate":
				n.Entries = append(n.Entries, n.Entries[0])
			case "bytes":
				n.Entries[0].Bytes = MaxReceiptBytes + 1
			case "window":
				n.Window.LockSHA = ""
			case "open":
				n.DispatchClosed = false
			case "dirty":
				n.RuntimeClean = false
			case "empty":
				n.Entries = nil
			case "order":
				n.Entries = append(n.Entries, ArchiveEntry{Name: "package-origin.json", SHA: strings.Repeat("c", 64), Bytes: 100})
			}
			if _, e := EncodeArchive(n); e == nil {
				t.Fatal("unbounded or unclosed archive accepted")
			}
		})
	}
	if _, e := ParseArchive(append(raw, '\n')); e == nil {
		t.Fatal("alternative bytes accepted")
	}
}
