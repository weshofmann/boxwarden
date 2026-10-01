//go:build (darwin || linux) && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"io"
	"os"
	"reflect"
	"sort"
)

type protectedHandle interface {
	io.Reader
	Stat() (os.FileInfo, error)
	Readdirnames(int) ([]string, error)
	Close() error
}
type protectedInspector struct {
	stat      func(string) (os.FileInfo, error)
	canonical func(string) (string, error)
	acl       func(string, os.FileInfo, string) error
	open      func(string, bool) (protectedHandle, error)
	volume    func(protectedHandle) (string, error)
}

func checkProtectedInventory(ctx context.Context, r contract.ProtectedInventory, i protectedInspector) error {
	if ctx.Err() != nil || !r.Valid() || i.stat == nil || i.canonical == nil || i.acl == nil || i.open == nil || i.volume == nil {
		return ErrRefused
	}
	entries := []contract.ProtectedFile{}
	for _, x := range r.Directories {
		entries = append(entries, contract.ProtectedFile{Metadata: x.Metadata})
	}
	for _, o := range r.Objects {
		entries = append(entries, o.Files[:]...)
	}
	validate := func(x contract.ImageMetadata, dir bool, aclKind string) (os.FileInfo, error) {
		f, e := i.stat(x.Path)
		p, pe := i.canonical(x.Path)
		if e != nil || pe != nil || p != x.Path || f == nil || i.acl(x.Path, f, aclKind) != nil || !matchesImageMetadata(f, x, dir) || ctx.Err() != nil {
			return nil, ErrRefused
		}
		return f, nil
	}
	for index, x := range entries {
		dir := index < len(r.Directories)
		aclKind := contract.NoExtendedACL
		if dir {
			aclKind = r.Directories[index].ACLKind
		}
		if _, e := validate(x.Metadata, dir, aclKind); e != nil {
			return e
		}
		f, e := i.open(x.Metadata.Path, dir)
		if e != nil || f == nil {
			return ErrRefused
		}
		work := func() error {
			info, e := f.Stat()
			if e != nil || !matchesImageMetadata(info, x.Metadata, dir) {
				return ErrRefused
			}
			if x.Metadata.Path == r.Root {
				uuid, e := i.volume(f)
				if e != nil || uuid != r.VolumeUUID {
					return ErrRefused
				}
			}
			var children []string
			switch {
			case x.Metadata.Path == r.Root:
				children = append(children, r.RootChildren...)
			case x.Metadata.Path == r.Root+"/vms":
				for _, o := range r.Objects {
					children = append(children, o.Name)
				}
			case dir && len(x.Metadata.Path) > len(r.Root+"/vms/"):
				children = []string{"config.json", "disk.img", "nvram.bin"}
			}
			if children != nil {
				got, e := f.Readdirnames(257)
				if e != nil && e != io.EOF || len(got) > 256 {
					return ErrRefused
				}
				tail, ee := f.Readdirnames(1)
				if len(tail) != 0 || ee != io.EOF {
					return ErrRefused
				}
				sort.Strings(got)
				sort.Strings(children)
				if !reflect.DeepEqual(got, children) {
					return ErrRefused
				}
			}
			if x.SHA != "" {
				h := sha256.New()
				n, e := io.Copy(h, io.LimitReader(f, 1<<20+1))
				if e != nil || uint64(n) != x.Metadata.Bytes || hex.EncodeToString(h.Sum(nil)) != x.SHA {
					return ErrRefused
				}
			}
			info, e = f.Stat()
			if e != nil || !matchesImageMetadata(info, x.Metadata, dir) {
				return ErrRefused
			}
			_, e = validate(x.Metadata, dir, aclKind)
			return e
		}
		we := work()
		ce := f.Close()
		if we != nil || ce != nil {
			return errors.Join(ErrRefused, we, ce)
		}
	}
	// Bracket the entire observation, not merely each local descriptor interval.
	for index, x := range entries {
		kind := contract.NoExtendedACL
		if index < len(r.Directories) {
			kind = r.Directories[index].ACLKind
		}
		if _, e := validate(x.Metadata, index < len(r.Directories), kind); e != nil {
			return e
		}
	}
	return ctx.Err()
}
