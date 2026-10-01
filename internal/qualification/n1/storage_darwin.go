//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"os"
	"syscall"
	"time"
)

func storage(ctx context.Context) error {
	if os.Getuid() != 0 || os.Geteuid() != 0 || fixed.CheckStateDirectory() != nil {
		return ErrRefused
	}
	mount := "/Volumes/BoxwardenAlphaQualification"
	var handles []*os.File
	closeAll := func() error {
		var err error
		for _, f := range handles {
			if f.Close() != nil {
				err = ErrRefused
			}
		}
		return err
	}
	fail := func() error { closeAll(); return ErrRefused }
	for _, path := range []string{contract.ConfigRoot + "/stock.enrolled.json", contract.StateRoot, mount} {
		fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if e != nil {
			return fail()
		}
		handles = append(handles, os.NewFile(uintptr(fd), "n1-storage"))
	}
	config, state, m := handles[0], handles[1], handles[2]
	info, e := state.Stat()
	if e != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return fail()
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 501 {
		return fail()
	}
	id, e := hostidentity.Observe(state)
	if e != nil || id.VolumeUUID != contract.VolumeUUID {
		return fail()
	}
	var a, b, c syscall.Statfs_t
	if syscall.Fstatfs(int(config.Fd()), &a) != nil || syscall.Fstatfs(int(state.Fd()), &b) != nil || syscall.Fstatfs(int(m.Fd()), &c) != nil || a.Fsid == b.Fsid || b.Fsid != c.Fsid {
		return fail()
	}
	name := make([]byte, 0, len(b.Mntonname))
	for _, x := range b.Mntonname {
		if x == 0 {
			break
		}
		name = append(name, byte(x))
	}
	if string(name) != mount {
		return fail()
	}
	runner := execx.OSRunner{MaxOutputBytes: 2 << 20, StrictStderr: true}
	query := func(path string, args []string) (map[string]any, error) {
		q, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		r, e := runner.Run(q, execx.Command{Path: path, Args: args, Env: fixed.Environment(true), Stdin: []byte{}})
		if e != nil || r.Truncated || !r.StderrComplete || r.Stderr != "" {
			return nil, ErrRefused
		}
		return parsePlist([]byte(r.Stdout))
	}
	d, e := query("/usr/sbin/diskutil", []string{"info", "-plist", mount})
	if e != nil {
		return fail()
	}
	h, e := query("/usr/bin/hdiutil", []string{"info", "-plist"})
	if e != nil || encryptedAssociation(d, h) != nil {
		return fail()
	}
	for i, path := range []string{contract.ConfigRoot + "/stock.enrolled.json", contract.StateRoot, mount} {
		visible, e := os.Lstat(path)
		opened, e2 := handles[i].Stat()
		if e != nil || e2 != nil || !sameFile(visible, opened) {
			return fail()
		}
	}
	if fixed.CheckStateDirectory() != nil {
		return fail()
	}
	if closeAll() != nil {
		return ErrRefused
	}
	return nil
}
