//go:build darwin && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"strings"
	"testing"
)

type protectedFakeHandle struct {
	info         imageInfo
	reader       *strings.Reader
	children     []string
	mode         string
	enumerations int
	diskReads    *int
	closed       *int
}

func (f *protectedFakeHandle) Read(b []byte) (int, error) {
	if strings.HasSuffix(f.info.x.Path, "disk.img") {
		*f.diskReads++
		return 0, ErrRefused
	}
	if f.mode == "content" {
		return 0, ErrRefused
	}
	return f.reader.Read(b)
}
func (f *protectedFakeHandle) Stat() (os.FileInfo, error) {
	if f.mode == "descriptor" {
		x := f.info
		x.x.Nlink++
		return x, nil
	}
	return f.info, nil
}
func (f *protectedFakeHandle) Readdirnames(int) ([]string, error) {
	f.enumerations++
	if f.enumerations == 1 {
		children := append([]string(nil), f.children...)
		if f.mode == "extra" {
			children = append(children, "unexpected")
		}
		return children, nil
	}
	if f.mode == "lost-eof" {
		return []string{"late-unknown"}, nil
	}
	return nil, io.EOF
}
func (f *protectedFakeHandle) Close() error {
	*f.closed++
	if f.mode == "close" {
		return ErrRefused
	}
	return nil
}
func TestProtectedInventoryDescriptorAdmissionAndStrictClosure(t *testing.T) {
	for _, mode := range []string{"positive", "lost-eof", "extra", "descriptor", "content", "close", "volume", "metadata", "acl", "canonical", "late-drift"} {
		t.Run(mode, func(t *testing.T) {
			r := protectedFixture()
			if !r.Valid() {
				t.Fatal("invalid positive record")
			}
			diskReads, closed, opens, stats := 0, 0, 0, 0
			lookup := func(p string) (imageInfo, bool) {
				for _, x := range r.Directories {
					if x.Metadata.Path == p {
						return imageInfo{x.Metadata, true}, true
					}
				}
				for _, o := range r.Objects {
					for _, x := range o.Files {
						if x.Metadata.Path == p {
							return imageInfo{x.Metadata, false}, true
						}
					}
				}
				return imageInfo{}, false
			}
			i := protectedInspector{canonical: func(p string) (string, error) {
				if mode == "canonical" {
					return p + "/x", nil
				}
				return p, nil
			}, acl: func(string, os.FileInfo, string) error {
				if mode == "acl" {
					return ErrRefused
				}
				return nil
			}, volume: func(protectedHandle) (string, error) {
				if mode == "volume" {
					return "wrong", nil
				}
				return r.VolumeUUID, nil
			}}
			i.stat = func(p string) (os.FileInfo, error) {
				stats++
				f, ok := lookup(p)
				if !ok {
					return nil, ErrRefused
				}
				if mode == "metadata" || mode == "late-drift" && opens == len(r.Directories)+3 {
					f.x.Inode++
				}
				return f, nil
			}
			i.open = func(p string, dir bool) (protectedHandle, error) {
				opens++
				f, ok := lookup(p)
				if !ok {
					return nil, ErrRefused
				}
				var children []string
				switch {
				case p == r.Root:
					children = r.RootChildren
				case p == r.Root+"/vms":
					children = []string{contract.BaseName}
				case p == r.Root+"/vms/"+contract.BaseName:
					children = []string{"config.json", "disk.img", "nvram.bin"}
				}
				return &protectedFakeHandle{info: f, reader: strings.NewReader("x"), children: children, mode: mode, diskReads: &diskReads, closed: &closed}, nil
			}
			e := checkProtectedInventory(t.Context(), r, i)
			if mode == "positive" {
				if e != nil || closed != len(r.Directories)+3 {
					t.Fatal(e, closed)
				}
			} else if e == nil {
				t.Fatal("incomplete/changed protected inventory admitted")
			}
			if diskReads != 0 || closed != opens {
				t.Fatal("disk bytes or lost owned close", diskReads, closed, opens)
			}
			_ = stats
		})
	}
}
func TestProtectedInventoryCanonicalPureFiniteRecord(t *testing.T) {
	r := protectedFixture()
	raw, _ := json.Marshal(r)
	if _, e := contract.ParseProtectedInventory(raw, contract.SHA(raw)); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*contract.ProtectedInventory){func(r *contract.ProtectedInventory) { r.Root = "/foreign" }, func(r *contract.ProtectedInventory) { r.VolumeUUID = "wrong" }, func(r *contract.ProtectedInventory) { r.Objects[0].Name = "../escape" }, func(r *contract.ProtectedInventory) { r.Objects[0].Name = "other" }, func(r *contract.ProtectedInventory) { r.Objects = append(r.Objects, r.Objects[0]) }, func(r *contract.ProtectedInventory) { r.Objects[0].Files[2].SHA = contract.SHA([]byte("disk")) }, func(r *contract.ProtectedInventory) { r.Objects[0].Files[0].SHA = "" }, func(r *contract.ProtectedInventory) { r.Directories = r.Directories[:len(r.Directories)-1] }, func(r *contract.ProtectedInventory) { r.RootChildren = append(r.RootChildren, "vms") }, func(r *contract.ProtectedInventory) { r.Objects[0].Files[0].Metadata.NoACL = false }} {
		x := protectedFixture()
		change(&x)
		raw, _ := json.Marshal(x)
		if _, e := contract.ParseProtectedInventory(raw, contract.SHA(raw)); e == nil {
			t.Fatal("foreign/duplicate/missing/disk-hash admitted")
		}
	}
}
