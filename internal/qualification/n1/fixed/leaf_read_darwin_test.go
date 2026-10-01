//go:build darwin

package fixed

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

type leafFixtureHandle struct {
	f      *os.File
	mode   string
	read   *bool
	closes *int
}

func (h *leafFixtureHandle) Read(b []byte) (int, error) { *h.read = true; return h.f.Read(b) }
func (h *leafFixtureHandle) Stat() (os.FileInfo, error) {
	f, e := h.f.Stat()
	if e == nil && *h.read {
		if h.mode != "flags-during-final-acl" {
			leafFixtureDrift(f, h.mode)
		}
	}
	return f, e
}
func (h *leafFixtureHandle) Close() error {
	*h.closes++
	e := h.f.Close()
	if h.mode == "close" {
		return ErrRefused
	}
	return e
}
func leafFixtureDrift(f os.FileInfo, mode string) {
	s := f.Sys().(*syscall.Stat_t)
	switch mode {
	case "ctime":
		s.Ctimespec.Sec++
	case "flags", "flags-during-final-acl":
		s.Flags ^= 32768
	case "uid":
		s.Uid++
	case "gid":
		s.Gid++
	case "links":
		s.Nlink++
	case "native-mode":
		s.Mode ^= 0020
	case "device":
		s.Dev++
	case "inode":
		s.Ino++
	case "mtime":
		s.Mtimespec.Sec++
	}
}
func TestFinalLeafSecurityChangesRefuseIdenticalBytes(t *testing.T) {
	for _, mode := range []string{"positive", "acl", "ctime", "flags", "close", "final-acl-error", "final-ancestry-error", "uid", "gid", "links", "native-mode", "device", "inode", "mtime", "flags-during-final-acl"} {
		t.Run(mode, func(t *testing.T) {
			d, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			p := filepath.Join(d, "static-input")
			want := []byte("nonsecret unchanged input")
			if e = os.WriteFile(p, want, 0600); e != nil {
				t.Fatal(e)
			}
			read, closes, acls, parents := false, 0, 0, 0
			finalACL := false
			i := fileReadChecks{ancestry: func(string) error {
				parents++
				if mode == "final-ancestry-error" && parents == 2 {
					return ErrRefused
				}
				return nil
			}, stat: func(p string) (os.FileInfo, error) {
				f, e := os.Lstat(p)
				if e == nil && read {
					if mode != "flags-during-final-acl" || finalACL {
						leafFixtureDrift(f, mode)
					}
				}
				return f, e
			}, acl: func(string, os.FileInfo) error {
				acls++
				if read {
					finalACL = true
				}
				if read && (mode == "acl" || mode == "final-acl-error") {
					return ErrRefused
				}
				return nil
			}, open: func(p string) (fileReadHandle, error) {
				fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
				if e != nil {
					return nil, e
				}
				return &leafFixtureHandle{os.NewFile(uintptr(fd), "private-leaf-fixture"), mode, &read, &closes}, nil
			}}
			got, e := readChecked(p, 1024, os.Getuid(), 0600, os.Getgid(), i)
			after, e2 := os.ReadFile(p)
			if e2 != nil || !bytes.Equal(after, want) {
				t.Fatal("fixture bytes changed")
			}
			if closes != 1 {
				t.Fatal("owned close lost", closes)
			}
			if mode == "positive" {
				if e != nil || !bytes.Equal(got, want) || acls != 2 || parents != 2 {
					t.Fatal(e, string(got))
				}
			} else if e == nil || got != nil {
				t.Fatal("security-only drift admitted with identical bytes", mode, string(got))
			}
		})
	}
}
