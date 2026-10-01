package contract

import (
	"fmt"
	"path"
)

const SudoPath = "/usr/bin/sudo"
const QualificationRoot = PackageRoot + "/qualification"

type ProtectedSudo struct {
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	QualificationSHA string `json:"qualification_sha"`
}

func (s ProtectedSudo) Valid() bool {
	return s.Kind == "protected-sudo" && s.Path == SudoPath && digest(s.QualificationSHA)
}

type Platform struct {
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	Release string `json:"release"`
	Build   string `json:"build"`
}

func (p Platform) Valid() bool { return p == (Platform{"darwin", "arm64", "27.0.1", "26A434"}) }

type ImageMetadata struct {
	Path    string `json:"path"`
	Device  uint64 `json:"device"`
	Inode   uint64 `json:"inode"`
	UID     uint32 `json:"uid"`
	GID     uint32 `json:"gid"`
	Mode    uint32 `json:"mode"`
	Nlink   uint64 `json:"nlink"`
	Bytes   uint64 `json:"bytes"`
	MtimeNS uint64 `json:"mtime_ns"`
	CtimeNS uint64 `json:"ctime_ns"`
	Flags   uint32 `json:"flags"`
	NoACL   bool   `json:"no_acl"`
}
type Qualification struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Path        string          `json:"path"`
	Platform    Platform        `json:"platform"`
	SHA         string          `json:"sha"`
	Ancestors   []ImageMetadata `json:"ancestors"`
	Leaf        ImageMetadata   `json:"leaf"`
	IntendedUse string          `json:"intended_use"`
}

func (q Qualification) Valid() bool {
	if !path.IsAbs(q.Path) || path.Clean(q.Path) != q.Path || q.Path == "/" || q.Version != 1 || !q.Platform.Valid() || q.Leaf.Path != q.Path || len(q.IntendedUse) < 1 || len(q.IntendedUse) > 128 || len(q.Ancestors) < 1 || len(q.Ancestors) > 16 {
		return false
	}
	parents := []string{}
	for p := path.Dir(q.Path); ; p = path.Dir(p) {
		parents = append(parents, p)
		if p == "/" {
			break
		}
	}
	if len(parents) != len(q.Ancestors) {
		return false
	}
	for i, a := range q.Ancestors {
		if a.Path != parents[len(parents)-1-i] || !metadataValid(a, true) {
			return false
		}
	}
	if !metadataValid(q.Leaf, false) {
		return false
	}
	if q.Kind == "protected-sudo" {
		return q.Path == SudoPath && q.SHA == "" && q.Leaf.Device == 16777230 && q.Leaf.Inode == 1152921500312608819 && q.Leaf.Mode == 04511 && q.Leaf.Flags == 524320 && q.Leaf.Bytes == 2362384 && q.Leaf.MtimeNS == 1790233837000000000 && q.Leaf.CtimeNS == 1790233837000000000 && exactSudoAncestors(q.Ancestors)
	}
	if q.Kind != "digest" || !digest(q.SHA) || q.Leaf.Mode != 0755 {
		return false
	}
	for _, p := range SystemPaths {
		if q.Path == p {
			return true
		}
	}
	return false
}
func metadataValid(a ImageMetadata, dir bool) bool {
	if a.Device == 0 || a.Inode == 0 || a.UID != 0 || a.GID != 0 || !a.NoACL || a.MtimeNS == 0 || a.CtimeNS == 0 || a.MtimeNS > 9223372036854775807 || a.CtimeNS > 9223372036854775807 || a.Bytes > 128<<20 || a.Nlink == 0 {
		return false
	}
	if dir {
		return a.Mode == 0755
	}
	return a.Nlink == 1 && a.Bytes > 0 && (a.Mode == 0755 || a.Mode == 04511)
}
func exactSudoAncestors(xs []ImageMetadata) bool {
	if len(xs) != 3 {
		return false
	}
	inos := [3]uint64{2, 1152921500312607501, 1152921500312607504}
	links := [3]uint64{23, 11, 934}
	sizes := [3]uint64{736, 352, 29888}
	flags := [3]uint32{1048576, 557056, 524288}
	for i, x := range xs {
		if x.Flags != flags[i] || x.Device != 16777230 || x.Inode != inos[i] || x.Nlink != links[i] || x.Bytes != sizes[i] || x.MtimeNS != 1790233837000000000 || x.CtimeNS != 1790233837000000000 {
			return false
		}
	}
	return true
}
func ParseQualification(raw []byte, kind, p, sha, qualificationSHA string) (Qualification, error) {
	var q Qualification
	if SHA(raw) != qualificationSHA || strict(raw, 8192, &q) != nil || !q.Valid() || q.Kind != kind || q.Path != p || q.SHA != sha {
		return Qualification{}, ErrRefused
	}
	return q, nil
}

// Only verified canonical received records can populate a catalogue. The zero
// value is not admitted; getters return detached ancestry to prevent mutation.
type Catalogue struct {
	records []Qualification
	hashes  []string
}

func (c Catalogue) Valid() bool {
	return len(c.records) == len(SystemPaths)+1 && len(c.hashes) == len(c.records)
}
func (c Catalogue) Lookup(p string) (Qualification, string, bool) {
	if !c.Valid() {
		return Qualification{}, "", false
	}
	for i, q := range c.records {
		if q.Path == p {
			q.Ancestors = append([]ImageMetadata(nil), q.Ancestors...)
			return q, c.hashes[i], true
		}
	}
	return Qualification{}, "", false
}
func QualificationPath(index int) string {
	if index < 0 || index > len(SystemPaths) {
		return ""
	}
	if index == 0 {
		return QualificationRoot + "/sudo.json"
	}
	return fmt.Sprintf("%s/system-%04d.json", QualificationRoot, index-1)
}
