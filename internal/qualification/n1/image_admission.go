//go:build (darwin || linux) && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"os"
	"path/filepath"
	"syscall"
)

type admittedImage struct {
	imageIdentity
	kind, qualification string
}
type imageInspector struct {
	stat      func(string) (os.FileInfo, error)
	canonical func(string) (string, error)
	acl       func(string, os.FileInfo) error
	platform  func() error
	hash      func(string) (imageIdentity, error)
}

func inspectProcessImage(ctx context.Context, p string, c contract.Catalogue) (admittedImage, error) {
	return admitImage(ctx, p, c, imageInspector{os.Lstat, filepath.EvalSymlinks, func(p string, f os.FileInfo) error { return pathmeta.Check(p, f, pathmeta.OSInspector{}) }, fixed.CheckPlatform, func(p string) (imageIdentity, error) { return readProcessImage(p, openProcessImage) }})
}
func admitImage(ctx context.Context, p string, c contract.Catalogue, i imageInspector) (admittedImage, error) {
	if !c.Valid() || ctx.Err() != nil || i.stat == nil || i.canonical == nil || i.acl == nil || i.platform == nil || i.hash == nil {
		return admittedImage{}, ErrRefused
	}
	q, key, qualified := c.Lookup(p)
	if !qualified {
		// No unreadable-path fallback; exact actor classification follows hashing.
		image, e := i.hash(p)
		if e != nil || ctx.Err() != nil {
			return admittedImage{}, ErrRefused
		}
		return admittedImage{image, "digest", ""}, nil
	}
	validate := func() error {
		if ctx.Err() != nil || i.platform() != nil {
			return ErrRefused
		}
		for n, x := range append(append([]contract.ImageMetadata(nil), q.Ancestors...), q.Leaf) {
			f, e := i.stat(x.Path)
			canonical, ce := i.canonical(x.Path)
			if e != nil || ce != nil || canonical != x.Path || f == nil || i.acl(x.Path, f) != nil || !matchesImageMetadata(f, x, n < len(q.Ancestors)) {
				return ErrRefused
			}
		}
		return nil
	}
	if validate() != nil {
		return admittedImage{}, ErrRefused
	}
	image := imageIdentity{sha: q.SHA, device: q.Leaf.Device, inode: q.Leaf.Inode}
	if q.Kind == "digest" {
		var e error
		image, e = i.hash(p)
		if e != nil || image.sha != q.SHA || image.device != q.Leaf.Device || image.inode != q.Leaf.Inode {
			return admittedImage{}, ErrRefused
		}
	}
	if validate() != nil {
		return admittedImage{}, ErrRefused
	}
	return admittedImage{image, q.Kind, key}, nil
}
func matchesImageMetadata(f os.FileInfo, x contract.ImageMetadata, dir bool) bool {
	st, ok := f.Sys().(*syscall.Stat_t)
	if !ok || f.IsDir() != dir || !dir && !f.Mode().IsRegular() {
		return false
	}
	return uint64(st.Dev) == x.Device && st.Ino == x.Inode && st.Uid == x.UID && st.Gid == x.GID && uint32(st.Mode)&07777 == x.Mode && uint64(st.Nlink) == x.Nlink && uint64(f.Size()) == x.Bytes && imageTimes(st, x)
}

func imageTimestamp(sec, nsec int64, want uint64) bool {
	if sec < 0 || nsec < 0 || nsec >= 1000000000 || uint64(sec) > (9223372036854775807-uint64(nsec))/1000000000 {
		return false
	}
	return uint64(sec)*1000000000+uint64(nsec) == want
}
