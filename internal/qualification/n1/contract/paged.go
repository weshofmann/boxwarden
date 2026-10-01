package contract

import "fmt"

const MaxOSIndexBytes = 8192
const MaxOSPageBytes = 65536
const MaxQualificationBytes = 8192
const MaxOSRejectionsBytes = 65536
const MaxCatalogueBytes = 8 << 20
const SystemPageCount = 7
const SystemPageEntries = 128
const OSIndexPath = QualificationRoot + "/system-index.json"
const OSRejectionsPath = QualificationRoot + "/system-rejections.json"

type OSIndex struct {
	Version int    `json:"version"`
	SHA     string `json:"sha"`
	Records int    `json:"records"`
	Pages   int    `json:"pages"`
}

func (x OSIndex) Valid() bool {
	return x.Version == 1 && digest(x.SHA) && x.Records == len(SystemPaths) && x.Pages == SystemPageCount
}

type OSPageBinding struct {
	Ordinal int    `json:"ordinal"`
	Records int    `json:"records"`
	SHA     string `json:"sha"`
}
type OSIndexRecord struct {
	Version       int             `json:"version"`
	Records       int             `json:"records"`
	Pages         []OSPageBinding `json:"pages"`
	RejectionsSHA string          `json:"rejections_sha"`
}
type OSImageEntry struct {
	Ordinal          int    `json:"ordinal"`
	Path             string `json:"path"`
	SHA              string `json:"sha"`
	QualificationSHA string `json:"qualification_sha"`
}
type OSPage struct {
	Version int            `json:"version"`
	Ordinal int            `json:"ordinal"`
	Entries []OSImageEntry `json:"entries"`
}

func SystemPagePath(i int) string {
	if i < 0 || i >= SystemPageCount {
		return ""
	}
	return fmt.Sprintf("%s/system-page-%02d.json", QualificationRoot, i)
}
func pageRecords(i int) int {
	if i < 0 || i >= SystemPageCount {
		return 0
	}
	if i == SystemPageCount-1 {
		return len(SystemPaths) - i*SystemPageEntries
	}
	return SystemPageEntries
}
func ParseOSIndex(raw []byte, binding OSIndex) (OSIndexRecord, error) {
	var x OSIndexRecord
	if !binding.Valid() || SHA(raw) != binding.SHA || strict(raw, MaxOSIndexBytes, &x) != nil || x.Version != 1 || x.Records != binding.Records || len(x.Pages) != binding.Pages || !digest(x.RejectionsSHA) {
		return OSIndexRecord{}, ErrRefused
	}
	for i, p := range x.Pages {
		if p.Ordinal != i || p.Records != pageRecords(i) || !digest(p.SHA) {
			return OSIndexRecord{}, ErrRefused
		}
	}
	return x, nil
}

func ParseOSPage(raw []byte, i int, binding OSPageBinding) (OSPage, error) {
	var p OSPage
	if i < 0 || i >= SystemPageCount || binding.Ordinal != i || binding.Records != pageRecords(i) || SHA(raw) != binding.SHA || strict(raw, MaxOSPageBytes, &p) != nil || p.Version != 1 || p.Ordinal != i || len(p.Entries) != pageRecords(i) {
		return OSPage{}, ErrRefused
	}
	for j, x := range p.Entries {
		ordinal := i*SystemPageEntries + j
		if x.Ordinal != ordinal || x.Path != SystemPaths[ordinal] || !digest(x.SHA) || !digest(x.QualificationSHA) {
			return OSPage{}, ErrRefused
		}
	}
	return p, nil
}

// Records are sudo followed by every compile-fixed ordinal. All inputs are
// received bytes, never caller-controlled filenames or runtime declarations.
type CatalogueInputs struct {
	Index      []byte
	Rejections []byte
	Pages      [][]byte
	Records    [][]byte
}

func AdmitCatalogue(s StaticLock, in CatalogueInputs) (Catalogue, error) {
	if !s.Valid() || len(in.Pages) != SystemPageCount || len(in.Records) != len(SystemPaths)+1 {
		return Catalogue{}, ErrRefused
	}
	index, e := ParseOSIndex(in.Index, s.OSIndex)
	if e != nil || len(in.Rejections) == 0 || len(in.Rejections) > MaxOSRejectionsBytes || SHA(in.Rejections) != index.RejectionsSHA {
		return Catalogue{}, ErrRefused
	}
	total := len(in.Index) + len(in.Rejections)
	entries := make([]OSImageEntry, 0, len(SystemPaths))
	for i, raw := range in.Pages {
		total += len(raw)
		if total > MaxCatalogueBytes {
			return Catalogue{}, ErrRefused
		}
		p, e := ParseOSPage(raw, i, index.Pages[i])
		if e != nil {
			return Catalogue{}, ErrRefused
		}
		entries = append(entries, p.Entries...)
	}

	c := Catalogue{}
	for i, raw := range in.Records {
		total += len(raw)
		if total > MaxCatalogueBytes {
			return Catalogue{}, ErrRefused
		}
		kind, p, sha, key := "protected-sudo", s.ProtectedSudo.Path, "", s.ProtectedSudo.QualificationSHA
		if i > 0 {
			x := entries[i-1]
			kind, p, sha, key = "digest", x.Path, x.SHA, x.QualificationSHA
		}
		q, e := ParseQualification(raw, kind, p, sha, key)
		if e != nil {
			return Catalogue{}, ErrRefused
		}
		c.records = append(c.records, q)
		c.hashes = append(c.hashes, key)
	}
	return c, nil
}
