//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func preflightStorage(ctx context.Context, mount string, s fixed.StaticInputs) error {
	runner := execx.OSRunner{MaxOutputBytes: 2 << 20, StrictStderr: true}
	query := func(p string, args []string) (map[string]any, error) {
		q, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		r, e := runner.Run(q, execx.Command{Path: p, Args: args, Env: fixed.Environment(true), Stdin: []byte{}})
		if e != nil || r.Truncated || !r.StderrComplete || r.Stderr != "" {
			return nil, ErrRefused
		}
		return parsePlist([]byte(r.Stdout))
	}
	d, e := query("/usr/sbin/diskutil", []string{"info", "-plist", mount})
	if e != nil {
		return ErrRefused
	}
	h, e := query("/usr/bin/hdiutil", []string{"info", "-plist"})
	if e != nil || encryptedAssociation(d, h) != nil {
		return ErrRefused
	}
	var volumeDevice uint64
	for i, p := range []string{mount, "/Users/devel/Library/Application Support/boxwarden/tart", contract.PackageRoot} {
		canonical, e := filepath.EvalSymlinks(p)
		before, se := os.Lstat(p)
		if e != nil || se != nil || canonical != p || !before.IsDir() || before.Mode() != os.ModeDir|0700 || pathmeta.Check(p, before, pathmeta.OSInspector{}) != nil {
			return ErrRefused
		}
		st, ok := before.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 501 {
			return ErrRefused
		}
		fd, e := syscall.Open(p, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e != nil {
			return ErrRefused
		}
		f := os.NewFile(uintptr(fd), "n1-preflight-storage")
		opened, oe := f.Stat()
		id, ie := hostidentity.Observe(f)
		ce := f.Close()
		after, ae := os.Lstat(p)
		if oe != nil || ie != nil || ce != nil || ae != nil || !sameFile(before, opened) || !sameFile(before, after) || pathmeta.Check(p, after, pathmeta.OSInspector{}) != nil {
			return ErrRefused
		}
		if i == 0 {
			if id.VolumeUUID != contract.VolumeUUID {
				return ErrRefused
			}
			volumeDevice = uint64(st.Dev)
		}
		if i == 1 && id.VolumeUUID != "568ee3b5-885b-4278-bd0e-5fe77c5d01a8" {
			return ErrRefused
		}
		if i == 2 && uint64(st.Dev) == volumeDevice {
			return ErrRefused
		}
	}
	return ctx.Err()
}
