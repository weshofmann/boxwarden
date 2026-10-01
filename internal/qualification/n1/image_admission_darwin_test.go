//go:build darwin && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"syscall"
	"testing"
	"time"
)

type imageInfo struct {
	x   contract.ImageMetadata
	dir bool
}

func (f imageInfo) Name() string { return f.x.Path }
func (f imageInfo) Size() int64  { return int64(f.x.Bytes) }
func (f imageInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0755
	}
	return 0755
}
func (f imageInfo) ModTime() time.Time { return time.Unix(0, int64(f.x.MtimeNS)) }
func (f imageInfo) IsDir() bool        { return f.dir }
func (f imageInfo) Sys() any {
	return &syscall.Stat_t{Dev: int32(f.x.Device), Ino: f.x.Inode, Uid: f.x.UID, Gid: f.x.GID, Mode: uint16(f.x.Mode), Nlink: uint16(f.x.Nlink), Mtimespec: syscall.Timespec{Sec: int64(f.x.MtimeNS / 1000000000), Nsec: int64(f.x.MtimeNS % 1000000000)}, Ctimespec: syscall.Timespec{Sec: int64(f.x.CtimeNS / 1000000000), Nsec: int64(f.x.CtimeNS % 1000000000)}, Flags: f.x.Flags}
}
func TestQualifiedSudoStructuralAdmissionNeverOpensBytes(t *testing.T) {
	_, c := censusFixture()
	q, _, _ := c.Lookup(contract.SudoPath)
	for _, stage := range []int{0, 1, 2} {
		for _, field := range []string{"positive", "device", "inode", "uid", "gid", "mode", "links", "size", "mtime", "ctime", "flags", "acl", "canonical", "platform", "stat"} {
			t.Run(field+string(rune('0'+stage)), func(t *testing.T) {
				calls, hashCalls := 0, 0
				i := imageInspector{canonical: func(p string) (string, error) {
					if field == "canonical" {
						return p + "/wrong", nil
					}
					return p, nil
				}, acl: func(string, os.FileInfo) error {
					if field == "acl" {
						return ErrRefused
					}
					return nil
				}, platform: func() error {
					if field == "platform" {
						return ErrRefused
					}
					return nil
				}, hash: func(string) (imageIdentity, error) {
					hashCalls++
					return imageIdentity{}, errors.New("byte read forbidden")
				}}
				i.stat = func(p string) (os.FileInfo, error) {
					calls++
					x := q.Leaf
					dir := false
					for _, a := range q.Ancestors {
						if a.Path == p {
							x = a
							dir = true
						}
					}
					mutate := stage == 0 || stage == 1 && calls <= 4 || stage == 2 && calls > 4
					if mutate {
						switch field {
						case "device":
							x.Device++
						case "inode":
							x.Inode++
						case "uid":
							x.UID++
						case "gid":
							x.GID++
						case "mode":
							x.Mode = 0777
						case "links":
							x.Nlink++
						case "size":
							x.Bytes++
						case "mtime":
							x.MtimeNS++
						case "ctime":
							x.CtimeNS++
						case "flags":
							x.Flags++
						case "stat":
							return nil, ErrRefused
						}
					}
					return imageInfo{x, dir}, nil
				}
				a, e := admitImage(t.Context(), contract.SudoPath, c, i)
				if hashCalls != 0 {
					t.Fatal("sudo byte open")
				}
				if field == "positive" {
					if e != nil || a.kind != "protected-sudo" || a.sha != "" || calls != 8 {
						t.Fatalf("positive: %#v %v calls%d", a, e, calls)
					}
				} else if e == nil {
					t.Fatal("drift admitted")
				}
			})
		}
	}
}
func TestOtherUnreadableImagesNeverReceiveSudoExemption(t *testing.T) {
	_, c := censusFixture()
	for _, p := range []string{"/unknown/sudo", contract.SystemPaths[0]} {
		q, _, known := c.Lookup(p)
		hashCalls := 0
		i := imageInspector{platform: func() error { return nil }, canonical: func(p string) (string, error) { return p, nil }, acl: func(string, os.FileInfo) error { return nil }, hash: func(string) (imageIdentity, error) { hashCalls++; return imageIdentity{}, syscall.EACCES }}
		i.stat = func(p string) (os.FileInfo, error) {
			for _, a := range q.Ancestors {
				if a.Path == p {
					return imageInfo{a, true}, nil
				}
			}
			return imageInfo{q.Leaf, false}, nil
		}
		if _, e := admitImage(context.Background(), p, c, i); e == nil || hashCalls != 1 {
			t.Fatalf("EACCES admitted known%v calls%d", known, hashCalls)
		}
	}
	for _, v := range []struct {
		s, n int64
		want uint64
	}{{-1, 0, 0}, {1, -1, 0}, {1, 1000000000, 2000000000}, {9223372036854775807, 0, 0}} {
		if imageTimestamp(v.s, v.n, v.want) {
			t.Fatal("invalid timestamp accepted")
		}
	}
}
