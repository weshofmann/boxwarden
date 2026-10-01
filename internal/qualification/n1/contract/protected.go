package contract

import (
	"path"
	"sort"
	"strings"
)

const TartRoot = "/Users/devel/Library/Application Support/boxwarden/tart"
const TartVolumeUUID = "568ee3b5-885b-4278-bd0e-5fe77c5d01a8"
const ProtectedRecordIndex = 32

// Independently hash-bound observation expectations, never effect authority.
// Existing base-admission evidence remains a different immutable file.
type ProtectedFile struct {
	Metadata ImageMetadata `json:"metadata"`
	SHA      string        `json:"sha"`
}
type ProtectedDirectory struct {
	Metadata ImageMetadata `json:"metadata"`
	ACLKind  string        `json:"acl_kind"`
}

const NoExtendedACL = "none"
const EveryoneDenyDelete = "everyone-deny-delete"

// Only these three literal ancestors have the reviewed restrictive ACL.
func ProtectedAncestorMode(p string) uint32 {
	switch p {
	case "/Users/devel":
		return 0750
	case "/Users/devel/Library", "/Users/devel/Library/Application Support":
		return 0700
	}
	return 0
}
func (d ProtectedDirectory) Valid() bool {
	x := d.Metadata
	if mode := ProtectedAncestorMode(x.Path); mode != 0 {
		return d.ACLKind == EveryoneDenyDelete && !x.NoACL && x.UID == 501 && x.GID == 20 && x.Mode == mode && protectedMetadataBase(x)
	}
	return d.ACLKind == NoExtendedACL && protectedMeta(x, true)
}

type ProtectedObject struct {
	Name  string           `json:"name"`
	Files [3]ProtectedFile `json:"files"`
}
type ProtectedInventory struct {
	Version      int                  `json:"version"`
	Root         string               `json:"root"`
	VolumeUUID   string               `json:"volume_uuid"`
	Device       uint64               `json:"device"`
	Directories  []ProtectedDirectory `json:"directories"`
	RootChildren []string             `json:"root_children"`
	Objects      []ProtectedObject    `json:"objects"`
}

func safeObject(n string) bool {
	if len(n) < 1 || len(n) > 128 || n == "." || n == ".." {
		return false
	}
	for _, c := range n {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
func (r ProtectedInventory) Valid() bool {
	if r.Version != 1 || r.Root != TartRoot || r.VolumeUUID != TartVolumeUUID || r.Device == 0 || len(r.Objects) < 1 || len(r.Objects) > 32 || len(r.RootChildren) < 1 || len(r.RootChildren) > 256 || !sort.StringsAreSorted(r.RootChildren) {
		return false
	}
	hasVMs := false
	for i, n := range r.RootChildren {
		if !safeObject(n) || i > 0 && n == r.RootChildren[i-1] {
			return false
		}
		hasVMs = hasVMs || n == "vms"
	}
	if !hasVMs {
		return false
	}
	paths := []string{}
	for p := r.Root; ; p = path.Dir(p) {
		paths = append([]string{p}, paths...)
		if p == "/" {
			break
		}
	}
	paths = append(paths, r.Root+"/vms")
	base := false
	last := ""
	for _, o := range r.Objects {
		if !protectedObjectID(o.Name) || o.Name <= last {
			return false
		}
		last = o.Name
		base = base || o.Name == BaseName
		dir := r.Root + "/vms/" + o.Name
		paths = append(paths, dir)
		for j, n := range []string{"config.json", "nvram.bin", "disk.img"} {
			x := o.Files[j]
			if x.Metadata.Path != dir+"/"+n || !protectedMeta(x.Metadata, false) || x.Metadata.Device != r.Device {
				return false
			}
			if j == 2 {
				if x.SHA != "" {
					return false
				}
			} else if !digest(x.SHA) || x.Metadata.Bytes > 1<<20 {
				return false
			}
		}
	}
	if !base || len(r.Directories) != len(paths) {
		return false
	}
	for i, x := range r.Directories {
		if x.Metadata.Path != paths[i] || !x.Valid() {
			return false
		}
		if strings.HasPrefix(x.Metadata.Path, r.Root) && (x.Metadata.Device != r.Device || x.Metadata.UID != 501) {
			return false
		}
	}
	return true
}
func protectedMetadataBase(x ImageMetadata) bool {
	return x.Device != 0 && x.Inode != 0 && x.Nlink != 0 && x.MtimeNS != 0 && x.CtimeNS != 0 && x.MtimeNS <= 9223372036854775807 && x.CtimeNS <= 9223372036854775807 && x.Mode&0022 == 0 && x.Mode&07000 == 0
}
func protectedMeta(x ImageMetadata, dir bool) bool {
	if x.Device == 0 || x.Inode == 0 || x.Nlink == 0 || x.MtimeNS == 0 || x.CtimeNS == 0 || x.MtimeNS > 9223372036854775807 || x.CtimeNS > 9223372036854775807 || !x.NoACL || x.Mode&0022 != 0 || x.Mode&07000 != 0 {
		return false
	}
	if dir {
		return (x.UID == 0 || x.UID == 501) && (x.Mode == 0700 || x.Mode == 0755)
	}
	return x.UID == 501 && x.Nlink == 1 && x.Bytes > 0 && x.Bytes <= 9223372036854775807 && (x.Mode == 0600 || x.Mode == 0644)
}
func ParseProtectedInventory(raw []byte, sha string) (ProtectedInventory, error) {
	var r ProtectedInventory
	if SHA(raw) != sha || strict(raw, MaxLockBytes, &r) != nil || !r.Valid() {
		return ProtectedInventory{}, ErrRefused
	}
	return r, nil
}

// Matches internal/backend/id.go's closed ASCII operand grammar without
// importing backend's host ACL/doctor graph into the pure contract package.
func protectedObjectID(n string) bool {
	alpha := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' }
	if len(n) < 1 || len(n) > 127 || !alpha(n[0]) {
		return false
	}
	for i := 1; i < len(n); i++ {
		if !alpha(n[i]) && n[i] != '-' {
			return false
		}
	}
	return true
}
