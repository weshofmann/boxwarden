package closeout

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

func verifyArchive(ctx context.Context, path string, uid int, acl aclInspector, h contract.Handoff) error {
	if ctx.Err() != nil || acl == nil || !h.Window.Valid() || h.ArchiveBytes == 0 || h.ArchiveBytes > contract.MaxArchiveBytes || h.ArchiveFiles < 2 || h.ArchiveFiles > contract.MaxArchiveFiles {
		return ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(path)
	before, se := os.Lstat(path)
	if e != nil || se != nil || canonical != path || before.Mode() != os.ModeDir|0700 {
		return ErrRefused
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || (scope{acl: acl}).aclCheck(path, before) != nil {
		return ErrRefused
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return ErrRefused
	}
	work := func() error {
		pinned, e := root.Stat(".")
		if e != nil || !sameFile(pinned, before) {
			return ErrRefused
		}
		raw, e := readNamed(ctx, root, path, contract.ArchiveName, uid, acl, contract.MaxReceiptBytes)
		if e != nil || contract.SHA(raw) != h.ArchiveSHA {
			return ErrRefused
		}
		m, e := contract.ParseArchive(raw)
		if e != nil || m.Window != h.Window || uint16(len(m.Entries)+1) != h.ArchiveFiles {
			return ErrRefused
		}
		total := uint64(len(raw))
		want := []string{contract.ArchiveName}
		for _, entry := range m.Entries {
			if ctx.Err() != nil {
				return ErrRefused
			}
			b, e := readNamed(ctx, root, path, entry.Name, uid, acl, contract.MaxReceiptBytes)
			if e != nil || len(b) != int(entry.Bytes) || contract.SHA(b) != entry.SHA || !json.Valid(b) {
				return ErrRefused
			}
			total += uint64(len(b))
			if total > contract.MaxArchiveBytes {
				return ErrRefused
			}
			want = append(want, entry.Name)
		}
		if total != h.ArchiveBytes {
			return ErrRefused
		}
		f, e := root.Open(".")
		if e != nil {
			return ErrRefused
		}
		names, re := f.Readdirnames(contract.MaxArchiveFiles + 1)
		tail, te := f.Readdirnames(1)
		after, ae := f.Stat()
		ce := f.Close()
		if (re != nil && re != io.EOF) || len(names) > contract.MaxArchiveFiles || len(tail) != 0 || te != io.EOF || ae != nil || ce != nil || !sameFile(before, after) {
			return ErrRefused
		}
		sort.Strings(names)
		sort.Strings(want)
		if len(names) != len(want) {
			return ErrRefused
		}
		for i := range names {
			if names[i] != want[i] {
				return ErrRefused
			}
		}
		visible, e := os.Lstat(path)
		pinned, pe := root.Stat(".")
		if e != nil || pe != nil || !sameFile(before, visible) || !sameFile(before, pinned) || (scope{acl: acl}).aclCheck(path, visible) != nil {
			return ErrRefused
		}
		final, e := root.Stat(".")
		if e != nil || !sameFile(before, final) || ctx.Err() != nil {
			return ErrRefused
		}
		return nil
	}()
	return errors.Join(work, root.Close())
}
func readNamed(ctx context.Context, root *os.Root, path, name string, uid int, acl aclInspector, cap int) (raw []byte, err error) {
	if ctx.Err() != nil || root == nil || acl == nil || filepath.Base(name) != name || name == "." || name == ".." || cap < 1 {
		return nil, ErrRefused
	}
	info, e := root.Lstat(name)
	if e != nil || info.Mode() != 0600 || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > int64(cap) {
		return nil, ErrRefused
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != uid || st.Nlink != 1 || (scope{acl: acl}).aclCheck(filepath.Join(path, name), info) != nil {
		return nil, ErrRefused
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, ErrRefused
	}
	defer func() {
		if f.Close() != nil {
			raw = nil
			err = ErrRefused
		}
	}()
	opened, e := f.Stat()
	if e != nil || !sameFile(opened, info) {
		return nil, ErrRefused
	}
	raw, e = io.ReadAll(io.LimitReader(f, int64(cap)+1))
	if e != nil || len(raw) > cap {
		return nil, ErrRefused
	}
	after, e := f.Stat()
	current, ce := root.Lstat(name)
	if e != nil || ce != nil || !sameFile(opened, after) || !sameFile(opened, current) || (scope{acl: acl}).aclCheck(filepath.Join(path, name), current) != nil {
		return nil, ErrRefused
	}
	final, e := f.Stat()
	visible, ve := root.Lstat(name)
	if e != nil || ve != nil || !sameFile(opened, final) || !sameFile(opened, visible) || ctx.Err() != nil {
		return nil, ErrRefused
	}
	return raw, nil
}
