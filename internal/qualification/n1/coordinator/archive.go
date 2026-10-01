package coordinator

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

type archive struct {
	root    string
	window  contract.Window
	entries []contract.ArchiveEntry
	bytes   uint64
	final   bool
}

func newArchive(root string, w contract.Window) (*archive, error) {
	if !w.Valid() || mkdirPrivate(root) != nil {
		return nil, ErrRefused
	}
	return &archive{root: root, window: w}, nil
}
func (a *archive) write(name string, raw []byte) error {
	if a == nil || a.final || !contract.ArchiveMember(name) || len(raw) == 0 || len(raw) > contract.MaxReceiptBytes || len(a.entries)+2 > contract.MaxArchiveFiles || a.bytes+uint64(len(raw)) > contract.MaxArchiveBytes {
		return ErrRefused
	}
	for _, e := range a.entries {
		if e.Name == name {
			return ErrRefused
		}
	}
	if exclusive(a.root, name, raw) != nil {
		return ErrRefused
	}
	a.entries = append(a.entries, contract.ArchiveEntry{Name: name, SHA: contract.SHA(raw), Bytes: uint32(len(raw))})
	a.bytes += uint64(len(raw))
	return nil
}
func (a *archive) finalize(dispatch, runtime bool) (string, uint16, uint64, error) {
	if a == nil || a.final {
		return "", 0, 0, ErrRefused
	}
	a.final = true
	sort.Slice(a.entries, func(i, j int) bool { return a.entries[i].Name < a.entries[j].Name })
	m := contract.ArchiveManifest{Version: 1, Window: a.window, DispatchClosed: dispatch, RuntimeClean: runtime, Entries: a.entries}
	raw, e := contract.EncodeArchive(m)
	if e != nil || a.bytes+uint64(len(raw)) > contract.MaxArchiveBytes {
		return "", 0, 0, ErrRefused
	}
	if a.exhaustive() != nil {
		return "", 0, 0, ErrRefused
	}
	for _, e := range a.entries {
		b, err := privateRead(filepath.Join(a.root, e.Name), contract.MaxReceiptBytes)
		if err != nil || uint32(len(b)) != e.Bytes || contract.SHA(b) != e.SHA {
			return "", 0, 0, ErrRefused
		}
	}
	if exclusive(a.root, contract.ArchiveName, raw) != nil {
		return "", 0, 0, ErrRefused
	}
	return contract.SHA(raw), uint16(len(a.entries) + 1), a.bytes + uint64(len(raw)), nil
}
func privateRead(p string, limit int) (raw []byte, err error) { return privateReadMode(p, limit, 0600) }
func privateReadMode(p string, limit int, mode os.FileMode) (raw []byte, err error) {
	before, e := os.Lstat(p)
	if e != nil || !before.Mode().IsRegular() || before.Mode() != mode || before.Size() < 1 || before.Size() > int64(limit) {
		return nil, ErrRefused
	}
	st, ok := before.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || st.Nlink != 1 || pathmeta.Check(p, before, pathmeta.OSInspector{}) != nil {
		return nil, ErrRefused
	}
	f, e := os.OpenFile(p, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
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
	if e != nil || !sameFile(before, opened) {
		return nil, ErrRefused
	}
	raw, e = io.ReadAll(io.LimitReader(f, int64(limit)+1))
	after, ae := f.Stat()
	visible, ve := os.Lstat(p)
	if e != nil || len(raw) > limit || ae != nil || ve != nil || !sameFile(opened, after) || !sameFile(opened, visible) || pathmeta.Check(p, visible, pathmeta.OSInspector{}) != nil {
		return nil, ErrRefused
	}
	return raw, nil
}
func sameFile(a, b os.FileInfo) bool {
	if a == nil || b == nil || !os.SameFile(a, b) || a.Mode() != b.Mode() || a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	x, ok := a.Sys().(*syscall.Stat_t)
	y, yes := b.Sys().(*syscall.Stat_t)
	return ok && yes && x.Uid == y.Uid && x.Gid == y.Gid && x.Nlink == y.Nlink && sameNativeSecurity(x, y)
}

func (a *archive) exhaustive() (err error) {
	f, e := os.OpenFile(a.root, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return ErrRefused
	}
	defer func() {
		if f.Close() != nil {
			err = ErrRefused
		}
	}()
	names, e := f.Readdirnames(contract.MaxArchiveFiles + 1)
	if e != nil && e != io.EOF || len(names) != len(a.entries) {
		return ErrRefused
	}
	allowed := map[string]bool{}
	for _, entry := range a.entries {
		allowed[entry.Name] = true
	}
	for _, name := range names {
		if !allowed[name] {
			return ErrRefused
		}
	}
	return nil
}
