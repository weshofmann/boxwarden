package closeout

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

const intentName = "closeout-intent.json"
const finalName = "closeout-receipt.json"

type closeoutRecord struct {
	Version            int              `json:"version"`
	Phase              string           `json:"phase"`
	Witness            contract.Witness `json:"witness"`
	ArchiveSHA         string           `json:"archive_sha"`
	Configs            [2]string        `json:"configs"`
	StateRoot          string           `json:"state_root"`
	StateRetired       bool             `json:"state_retired"`
	ConfigsRetired     bool             `json:"configs_retired"`
	StockDoctorHealthy bool             `json:"stock_doctor_healthy"`
	ProtectedUnchanged bool             `json:"protected_unchanged"`
}

func recordBytes(w contract.Witness, h contract.Handoff, complete bool) ([]byte, error) {
	if !w.Valid() || w.Phase != 3 || !h.Valid() || w.WindowID != h.Window.ID || w.LockSHA != h.Window.LockSHA {
		return nil, ErrRefused
	}
	phase := "reserved"
	if complete {
		phase = "complete"
	}
	return contract.Encode(closeoutRecord{Version: 1, Phase: phase, Witness: w, ArchiveSHA: h.ArchiveSHA, Configs: h.Configs, StateRoot: contract.StateRoot, StateRetired: complete, ConfigsRetired: complete, StockDoctorHealthy: complete, ProtectedUnchanged: complete})
}
func directoryMembers(root *os.Root) (names []string, err error) {
	f, e := root.Open(".")
	if e != nil {
		return nil, ErrRefused
	}
	names, re := f.Readdirnames(257)
	tail, te := f.Readdirnames(1)
	ce := f.Close()
	if (re != nil && re != io.EOF) || len(names) > 256 || len(tail) != 0 || te != io.EOF || ce != nil {
		return nil, ErrRefused
	}
	sort.Strings(names)
	return names, nil
}
func publishRecord(ctx context.Context, path string, uid int, acl aclInspector, name string, raw []byte, tick func() error, closeFile func(*os.File) error, syncFile func(*os.File) error, write func(*os.File, []byte) (int, error)) (err error) {
	if ctx.Err() != nil || tick == nil || tick() != nil || acl == nil || closeFile == nil || syncFile == nil || write == nil || (name != intentName && name != finalName) || len(raw) == 0 || len(raw) > contract.MaxReceiptBytes {
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
	defer func() { err = errors.Join(err, root.Close()) }()
	pinned, e := root.Stat(".")
	if e != nil || !sameFile(pinned, before) {
		return ErrRefused
	}
	names, e := directoryMembers(root)
	if e != nil {
		return ErrRefused
	}
	for _, n := range names {
		if n == name {
			return ErrRefused
		}
	}
	if ctx.Err() != nil || tick() != nil {
		return ErrRefused
	}
	f, e := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrRefused
	}
	n, we := write(f, raw)
	se = syncFile(f)
	info, ie := f.Stat()
	ce := closeFile(f)
	if we != nil || se != nil || ie != nil || ce != nil || n != len(raw) || info == nil {
		return errors.Join(ErrRefused, we, se, ie, ce)
	}
	b, e := readNamed(ctx, root, path, name, uid, acl, contract.MaxReceiptBytes)
	current, ce := root.Lstat(name)
	if e != nil || ce != nil || !sameFile(info, current) || contract.SHA(b) != contract.SHA(raw) {
		return ErrRefused
	}
	want := append(append([]string(nil), names...), name)
	sort.Strings(want)
	got, e := directoryMembers(root)
	if e != nil || len(got) != len(want) {
		return ErrRefused
	}
	for i := range got {
		if got[i] != want[i] {
			return ErrRefused
		}
	}
	after, e := root.Stat(".")
	visible, ve := os.Lstat(path)
	if e != nil || ve != nil || !stableDirectory(before, after) || !sameFile(after, visible) || (scope{acl: acl}).aclCheck(path, visible) != nil {
		return ErrRefused
	}
	dir, e := root.Open(".")
	if e != nil {
		return ErrRefused
	}
	se = syncFile(dir)
	ce = closeFile(dir)
	if se != nil || ce != nil || ctx.Err() != nil || tick() != nil {
		return errors.Join(ErrRefused, se, ce)
	}
	final, e := root.Stat(".")
	visible, ve = os.Lstat(path)
	if e != nil || ve != nil || !sameFile(after, final) || !sameFile(final, visible) {
		return ErrRefused
	}
	return nil
}
