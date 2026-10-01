package contract

import "sort"

const ArchiveName = "archive-index.json"
const ArchiveDirectory = "archive"
const MaxArchiveBytes = 2 << 20
const MaxArchiveFiles = 128

type ArchiveEntry struct {
	Name  string `json:"name"`
	SHA   string `json:"sha"`
	Bytes uint32 `json:"bytes"`
}
type ArchiveManifest struct {
	Version        int            `json:"version"`
	Window         Window         `json:"window"`
	DispatchClosed bool           `json:"dispatch_closed"`
	RuntimeClean   bool           `json:"runtime_clean"`
	Entries        []ArchiveEntry `json:"entries"`
}

// Names are a finite source declaration, never receipt-selected paths.
func ArchiveMember(n string) bool {
	switch n {
	case "package-origin.json", "runtime-initial.json", "runtime-final.json", "clipboard-verdict.json", "network-verdict.json",
		"receipt-enroll.json", "receipt-domain-init.json", "receipt-install.json",
		"receipt-control-create.json", "receipt-candidate-create.json", "receipt-control-start.json", "receipt-candidate-start.json",
		"receipt-control-stage.json", "receipt-candidate-stage.json", "receipt-control-restart.json", "receipt-candidate-restart.json",
		"receipt-initial-review.json", "receipt-final-review.json", "receipt-control-copy.json", "receipt-control-read.json",
		"receipt-candidate-copy.json", "receipt-candidate-read.json", "receipt-network-controls-before.json", "receipt-network-controls-after.json",
		"receipt-positive-before.json", "receipt-positive-after.json", "receipt-observer-control.json", "receipt-observer-candidate.json",
		"receipt-arm.json", "receipt-connect.json", "receipt-collect.json", "receipt-control-stop.json", "receipt-candidate-stop.json",
		"receipt-control-delete.json", "receipt-candidate-delete.json", "receipt-archive.json":
		return true
	}
	return false
}
func (m ArchiveManifest) Valid() bool {
	if m.Version != 1 || !m.Window.Valid() || !m.DispatchClosed || !m.RuntimeClean || len(m.Entries) < 1 || len(m.Entries)+1 > MaxArchiveFiles {
		return false
	}
	names := make([]string, len(m.Entries))
	var total uint64
	for i, e := range m.Entries {
		if !ArchiveMember(e.Name) || !digest(e.SHA) || e.Bytes == 0 || e.Bytes > MaxReceiptBytes || i > 0 && e.Name <= m.Entries[i-1].Name {
			return false
		}
		names[i] = e.Name
		total += uint64(e.Bytes)
	}
	return sort.StringsAreSorted(names) && total <= MaxArchiveBytes
}
func EncodeArchive(m ArchiveManifest) ([]byte, error) {
	if !m.Valid() {
		return nil, ErrRefused
	}
	return Encode(m)
}
func ParseArchive(raw []byte) (ArchiveManifest, error) {
	var m ArchiveManifest
	if strict(raw, MaxReceiptBytes, &m) != nil || !m.Valid() {
		return ArchiveManifest{}, ErrRefused
	}
	return m, nil
}
