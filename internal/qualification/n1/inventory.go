package n1

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
)

var ErrRefused = errors.New("n1 cleanup refused or unknown")

type scope struct {
	root    string
	uid     int
	acl     hostx.ACLInspector
	cap     int
	readDir func(*os.File, int) ([]string, error)
}
type inventoryGuard struct {
	root      *os.Root
	locks     []*os.File
	snapshots map[string]os.FileInfo
	scope     scope
	handoff   contract.Handoff
	closed    bool
	closeErr  error
}

func inventory(ctx context.Context, s scope, h contract.Handoff) (*inventoryGuard, error) {
	if ctx.Err() != nil || s.acl == nil || s.uid < 0 {
		return nil, ErrRefused
	}
	canonical, e := filepath.EvalSymlinks(s.root)
	if e != nil || canonical != s.root {
		return nil, ErrRefused
	}
	root, e := os.OpenRoot(s.root)
	if e != nil {
		return nil, ErrRefused
	}
	g := &inventoryGuard{root: root, scope: s, handoff: h, snapshots: map[string]os.FileInfo{}}
	if e = g.scan(ctx, true); e != nil {
		return nil, errors.Join(ErrRefused, g.Close())
	}
	return g, nil
}
func (g *inventoryGuard) scan(ctx context.Context, acquire bool) error {
	if g.closed || ctx.Err() != nil {
		return ErrRefused
	}
	visible, e := os.Lstat(g.scope.root)
	pinned, e2 := g.root.Stat(".")
	if e != nil || e2 != nil || !sameFile(visible, pinned) {
		return ErrRefused
	}
	s := g.scope
	cap := s.cap
	if cap == 0 {
		cap = 256
	}
	if cap < 1 || cap > 256 {
		return ErrRefused
	}
	required := map[string]os.FileMode{".": 0700, "identity": 0700, "identity/ssh-user-ca": 0700, "identity/ssh-host-pins": 0700, "goldens": 0700, "goldens/records": 0700, "sessions": 0700, "runtime": 0700, "runtime/n1qualification": 0700, "locks": 0700, "identity/ssh-user-ca/ca": 0600, "identity/ssh-user-ca/ca.pub": 0644}
	for _, p := range g.handoff.Pair {
		if !contract.UUID(p.Session) {
			return ErrRefused
		}
		required["runtime/n1qualification/"+p.Session] = 0700
	}
	public := map[string]string{}
	pins := map[string]contract.Peer{}
	for _, peer := range g.handoff.Pair {
		if !contract.UUID(peer.Session) || len(peer.HostPinSHA) != 64 {
			return ErrRefused
		}
		name := "identity/ssh-host-pins/" + peer.Session + ".json"
		required[name] = 0600
		public[name] = peer.HostPinSHA
		pins[name] = peer
	}
	for i, p := range g.handoff.PublicRecords {
		if p.Name != contract.PublicRecordNames[i] || p.WindowID != g.handoff.Window.ID || len(p.SHA) != 64 {
			return ErrRefused
		}
		required[p.Name] = 0600
		public[p.Name] = p.SHA
	}
	allowedLocks := map[string]bool{}
	for _, n := range []string{"golden.lock", "storage-n1qualification.lock", "session-n1qualification-n1diag20260930control.lock", "session-n1qualification-n1diag20260930candidate.lock", "transition-n1qualification-n1diag20260930control.lock", "transition-n1qualification-n1diag20260930candidate.lock"} {
		allowedLocks["locks/"+n] = true
	}
	// These known namespaces may remain only as empty directories. No residue,
	// hidden staging entry, socket, credential, quarantine or foreign session is accepted.
	optionalDirs := map[string]bool{"workspaces": true, "volumes": true, "rebuilds": true, "recipe-intents": true, "action-attempts": true}
	var ca contract.CAPublic
	var publicKey []byte
	seen := map[string]bool{}
	total := 0
	var visit func(string, int) error
	visit = func(name string, depth int) error {
		if ctx.Err() != nil || depth > 8 {
			return ErrRefused
		}
		total++
		if total > 512 {
			return ErrRefused
		}
		info, e := g.root.Lstat(name)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return ErrRefused
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(st.Uid) != s.uid {
			return ErrRefused
		}
		rootInfo, e := g.root.Stat(".")
		if e != nil {
			return ErrRefused
		}
		rs, ok := rootInfo.Sys().(*syscall.Stat_t)
		if !ok || st.Dev != rs.Dev {
			return ErrRefused
		}
		has, e := s.acl.HasExtendedACL(filepath.Join(s.root, name))
		if e != nil || has {
			return ErrRefused
		}
		mode, wanted := required[name]
		isLock := allowedLocks[name]
		optional := optionalDirs[name]
		if !wanted && !isLock && !optional {
			return ErrRefused
		}
		if isLock {
			mode = 0600
		}
		if optional {
			mode = 0700
		}
		if info.Mode().Perm() != mode || info.Mode()&(os.ModeSetuid|os.ModeSetgid|os.ModeSticky) != 0 {
			return ErrRefused
		}
		shouldDir := mode == 0700
		if info.IsDir() != shouldDir || !shouldDir && (!info.Mode().IsRegular() || st.Nlink != 1) {
			return ErrRefused
		}
		if old, ok := g.snapshots[name]; ok && !sameFile(old, info) {
			return ErrRefused
		}
		seen[name] = true
		if shouldDir {
			f, e := g.root.Open(name)
			if e != nil {
				return ErrRefused
			}
			fi, e := f.Stat()
			if e != nil || !sameFile(fi, info) {
				f.Close()
				return ErrRefused
			}
			rd := s.readDir
			if rd == nil {
				rd = func(f *os.File, n int) ([]string, error) { return f.Readdirnames(n) }
			}
			names, re := rd(f, cap+1)
			ce := f.Close()
			if (re != nil && re != io.EOF) || ce != nil || len(names) > cap {
				return ErrRefused
			}
			sort.Strings(names)
			if optional && len(names) != 0 {
				return ErrRefused
			}
			for _, child := range names {
				if child == "" || child == "." || child == ".." || filepath.Base(child) != child {
					return ErrRefused
				}
				next := child
				if name != "." {
					next = name + "/" + child
				}
				if e = visit(next, depth+1); e != nil {
					return e
				}
			}
		} else if sha, ok := public[name]; ok || name == "identity/ssh-user-ca/ca.pub" {
			f, e := g.root.Open(name)
			if e != nil {
				return ErrRefused
			}
			fi, e := f.Stat()
			if e != nil || !sameFile(fi, info) {
				f.Close()
				return ErrRefused
			}
			raw, re := io.ReadAll(io.LimitReader(f, 65537))
			after, se := f.Stat()
			ce := f.Close()
			if re != nil || se != nil || ce != nil || len(raw) > 65536 || !sameFile(fi, after) || name != "identity/ssh-user-ca/ca.pub" && contract.SHA(raw) != sha {
				return ErrRefused
			}

			if peer, ok := pins[name]; ok {
				if _, e := contract.ParseHostPin(raw, peer); e != nil {
					return ErrRefused
				}
			}
			switch name {
			case "identity/ssh-user-ca/ca.pub":
				publicKey = raw
			case contract.PublicRecordNames[0]:
				var e error
				ca, e = contract.ParseCAPublic(raw)
				if e != nil {
					return ErrRefused
				}
			case contract.PublicRecordNames[1]:
				if _, e := contract.ParseBase(raw); e != nil {
					return ErrRefused
				}
			}
		} else if isLock {
			if info.Size() != 0 {
				return ErrRefused
			}
			if acquire {
				f, e := g.root.Open(name)
				if e != nil {
					return ErrRefused
				}
				g.locks = append(g.locks, f)
				fi, e := f.Stat()
				if e != nil || !sameFile(fi, info) || syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
					return ErrRefused
				}
			}
		}
		after, e := g.root.Lstat(name)
		if e != nil || !sameFile(info, after) {
			return ErrRefused
		}
		if acquire {
			g.snapshots[name] = after
		}
		return nil
	}
	if e := visit(".", 0); e != nil {
		return e
	}
	if contract.MatchPublicKey(publicKey, ca) != nil {
		return ErrRefused
	}
	for name := range required {
		if !seen[name] {
			return ErrRefused
		}
	}
	if !acquire {
		if len(seen) != len(g.snapshots) {
			return ErrRefused
		}
		for n := range g.snapshots {
			if !seen[n] {
				return ErrRefused
			}
		}
	}
	return nil
}
func sameFile(a, b os.FileInfo) bool {
	if a == nil || b == nil || !os.SameFile(a, b) || a.Mode() != b.Mode() || a.Size() != b.Size() || !a.ModTime().Equal(b.ModTime()) {
		return false
	}
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Uid == y.Uid && x.Gid == y.Gid && x.Nlink == y.Nlink
}
func (g *inventoryGuard) Revalidate(ctx context.Context) error { return g.scan(ctx, false) }
func (g *inventoryGuard) Close() error {
	if g == nil {
		return nil
	}
	if g.closed {
		return g.closeErr
	}
	g.closed = true
	var e error
	for i := len(g.locks) - 1; i >= 0; i-- {
		e = errors.Join(e, g.locks[i].Close())
	}
	if g.root != nil {
		e = errors.Join(e, g.root.Close())
	}
	g.closeErr = e
	return e
}
