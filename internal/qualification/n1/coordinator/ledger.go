package coordinator

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/pathmeta"
	"os"
	"path/filepath"
	"syscall"
)

var ErrRefused = errors.New("n1 coordinator refused or unknown")
var phases = []string{"enroll", "domain-init", "install", "control-create", "candidate-create", "control-start", "candidate-start", "control-stage", "candidate-stage", "control-restart", "candidate-restart", "initial-review", "final-review", "control-copy", "control-read", "candidate-copy", "candidate-read", "network-controls-before", "network-controls-after", "positive-before", "positive-after", "observer-control", "observer-candidate", "arm", "connect", "collect", "control-stop", "candidate-stop", "control-delete", "candidate-delete", "archive"}

func phaseValid(s string) bool {
	for _, p := range phases {
		if p == s {
			return true
		}
	}
	return false
}
func uuid() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", ErrRefused
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:], nil
}

type ledger struct {
	root     string
	window   contract.Window
	attempts []contract.Attempt
	closed   bool
	fresh    int
}

func newLedger(root string, w contract.Window) (*ledger, error) {
	if !w.Valid() || mkdirPrivate(root) != nil {
		return nil, ErrRefused
	}
	raw, e := contract.Encode(w)
	if e != nil || exclusive(root, "origin.json", raw) != nil {
		return nil, ErrRefused
	}
	return &ledger{root: root, window: w}, nil
}
func (l *ledger) reserve(command string) (contract.Attempt, error) {
	if l == nil || l.closed || !phaseValid(command) || len(l.attempts) >= 32 {
		return contract.Attempt{}, ErrRefused
	}
	for _, a := range l.attempts {
		if a.Command == command {
			return contract.Attempt{}, ErrRefused
		}
	}
	fresh := command == "control-create" || command == "candidate-create"
	if fresh && l.fresh >= 2 {
		return contract.Attempt{}, ErrRefused
	}
	id, e := uuid()
	if e != nil {
		return contract.Attempt{}, e
	}
	a := contract.Attempt{ID: id, Command: command}
	raw, e := json.Marshal(a)
	if e != nil || exclusive(l.root, "reserved-"+command+".json", raw) != nil {
		return contract.Attempt{}, ErrRefused
	}
	l.attempts = append(l.attempts, a)
	if fresh {
		l.fresh++
	}
	return a, nil
}
func (l *ledger) closeAttempt(id string) error {
	for i, a := range l.attempts {
		if a.ID == id && !a.Closed {
			a.Closed = true
			raw, e := json.Marshal(a)
			if e != nil || exclusive(l.root, "closed-"+a.Command+".json", raw) != nil {
				return ErrRefused
			}
			l.attempts[i] = a
			return nil
		}
	}
	return ErrRefused
}
func (l *ledger) closeDispatch() error {
	if l.closed {
		return ErrRefused
	}
	l.closed = true
	return exclusive(l.root, "dispatch-closed.json", []byte(`{"closed":true}`))
}

// One-link private files are published once, fsynced, reopened and compared.
// The supplied paths are private test seams; production callers use fixed roots.
func exclusive(root, name string, raw []byte) (err error) {
	if len(raw) == 0 || len(raw) > contract.MaxReceiptBytes || filepath.Base(name) != name {
		return ErrRefused
	}
	d, e := os.OpenFile(root, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return ErrRefused
	}
	defer func() {
		if d.Close() != nil {
			err = ErrRefused
		}
	}()
	di, e := d.Stat()
	visible, e2 := os.Lstat(root)
	if e != nil || e2 != nil || !di.IsDir() || di.Mode() != os.ModeDir|0700 || !os.SameFile(di, visible) {
		return ErrRefused
	}
	st, ok := di.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || pathmeta.Check(root, visible, pathmeta.OSInspector{}) != nil {
		return ErrRefused
	}
	handle, e := os.OpenRoot(root)
	if e != nil {
		return ErrRefused
	}
	defer func() {
		if handle.Close() != nil {
			err = ErrRefused
		}
	}()
	p := filepath.Join(root, name)
	f, e := handle.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return ErrRefused
	}
	n, we := f.Write(raw)
	se := f.Sync()
	fi, fe := f.Stat()
	ce := f.Close()
	if we != nil || se != nil || fe != nil || ce != nil || n != len(raw) || fi.Mode() != 0600 || d.Sync() != nil {
		return ErrRefused
	}
	got, e := privateRead(p, contract.MaxReceiptBytes)
	if e != nil || contract.SHA(got) != contract.SHA(raw) {
		return ErrRefused
	}
	after, e := os.Lstat(p)
	if e != nil || !os.SameFile(fi, after) {
		return ErrRefused
	}
	current, e := os.Lstat(root)
	if e != nil || !os.SameFile(di, current) || pathmeta.Check(root, current, pathmeta.OSInspector{}) != nil {
		return ErrRefused
	}
	return nil
}

func mkdirPrivate(p string) error {
	if os.Mkdir(p, 0700) != nil {
		return ErrRefused
	}
	parent := filepath.Dir(p)
	f, e := os.OpenFile(parent, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if e != nil {
		return ErrRefused
	}
	se := f.Sync()
	ce := f.Close()
	if se != nil || ce != nil {
		return ErrRefused
	}
	return nil
}
