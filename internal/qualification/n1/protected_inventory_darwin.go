//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"os"
	"path/filepath"
	"syscall"
)

func observeProtectedInventory(ctx context.Context, r contract.ProtectedInventory) error {
	return checkProtectedInventory(ctx, r, protectedInspector{stat: os.Lstat, canonical: filepath.EvalSymlinks, acl: func(p string, f os.FileInfo, kind string) error {
		if kind == contract.EveryoneDenyDelete {
			return pathmeta.CheckQualificationAncestor(p, f, pathmeta.OSInspector{}, fixed.CheckPlatform)
		}
		return pathmeta.Check(p, f, pathmeta.OSInspector{})
	}, open: func(p string, dir bool) (protectedHandle, error) {
		flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC
		if dir {
			flags |= syscall.O_DIRECTORY
		}
		fd, e := syscall.Open(p, flags, 0)
		if e != nil {
			return nil, e
		}
		return os.NewFile(uintptr(fd), "n1-protected-observation"), nil
	}, volume: func(f protectedHandle) (string, error) {
		actual, ok := f.(*os.File)
		if !ok {
			return "", ErrRefused
		}
		id, e := hostidentity.Observe(actual)
		return id.VolumeUUID, e
	}})
}
