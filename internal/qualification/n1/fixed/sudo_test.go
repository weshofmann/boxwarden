package fixed

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/execx"
	"os"
	"reflect"
	"syscall"
	"testing"
	"time"
)

type sudoInfo struct {
	path string
	mode os.FileMode
	st   syscall.Stat_t
}

func (i sudoInfo) Name() string { return i.path }
func (i sudoInfo) Size() int64 {
	if i.mode.IsRegular() {
		return 2362384
	}
	return 0
}
func (i sudoInfo) Mode() os.FileMode  { return i.mode }
func (i sudoInfo) ModTime() time.Time { return time.Unix(1, 0) }
func (i sudoInfo) IsDir() bool        { return i.mode.IsDir() }
func (i sudoInfo) Sys() any           { return &i.st }

type sudoFixture struct {
	files               map[string]sudoInfo
	phase               int
	mutate              func(*sudoFixture)
	platformErr, aclErr error
	canonical           string
}

func newSudoFixture() *sudoFixture {
	f := &sudoFixture{files: map[string]sudoInfo{}, canonical: "/usr/bin/sudo"}
	for n, p := range []string{"/", "/usr", "/usr/bin", "/usr/bin/sudo"} {
		mode := os.ModeDir | 0755
		if n == 3 {
			mode = os.ModeSetuid | 0511
		}
		f.files[p] = sudoInfo{p, mode, syscall.Stat_t{Dev: 1, Ino: uint64(n + 1), Nlink: 1}}
	}
	return f
}
func (f *sudoFixture) stat(p string) (os.FileInfo, error) {
	i, ok := f.files[p]
	if !ok {
		return nil, errors.New("missing")
	}
	return i, nil
}
func (f *sudoFixture) acl(string, os.FileInfo) error { return f.aclErr }
func (f *sudoFixture) canon(p string) (string, error) {
	if p == "/usr/bin/sudo" {
		return f.canonical, nil
	}
	return p, nil
}
func (f *sudoFixture) platform() error {
	f.phase++
	if f.mutate != nil {
		f.mutate(f)
	}
	return f.platformErr
}
func (f *sudoFixture) inspector() sudoInspector {
	return sudoInspector{f.stat, f.acl, f.canon, f.platform}
}
func TestProtectedSudoStructuralAdmission(t *testing.T) {
	f := newSudoFixture()
	if e := checkSudo(f.inspector()); e != nil {
		t.Fatal("root:wheel04511 unreadable system executable must admit structurally", e)
	}
	for _, name := range []string{"mode", "owner", "group", "links", "symlink", "ancestor-owner", "ancestor-write", "acl", "platform", "canonical", "replacement", "after-mode"} {
		t.Run(name, func(t *testing.T) {
			f := newSudoFixture()
			i := f.files["/usr/bin/sudo"]
			switch name {
			case "mode":
				i.mode = 0555
			case "owner":
				i.st.Uid = 501
			case "group":
				i.st.Gid = 501
			case "links":
				i.st.Nlink = 2
			case "symlink":
				i.mode = os.ModeSymlink | 0777
			case "ancestor-owner":
				a := f.files["/usr"]
				a.st.Uid = 501
				f.files["/usr"] = a
			case "ancestor-write":
				a := f.files["/usr/bin"]
				a.mode = os.ModeDir | 0775
				f.files["/usr/bin"] = a
			case "acl":
				f.aclErr = errors.New("ACL")
			case "platform":
				f.platformErr = errors.New("unknown release/build")
			case "canonical":
				f.canonical = "/other/sudo"
			case "replacement", "after-mode":
				f.mutate = func(f *sudoFixture) {
					a := f.files["/usr/bin/sudo"]
					if name == "replacement" {
						a.st.Ino++
					} else {
						a.mode = 0555
					}
					f.files["/usr/bin/sudo"] = a
				}
			}
			f.files["/usr/bin/sudo"] = i
			if checkSudo(f.inspector()) == nil {
				t.Fatal("unsafe structural state admitted")
			}
		})
	}
}

type sudoProbe struct {
	results  []execx.Result
	err      error
	commands []execx.Command
}

func (r *sudoProbe) Run(_ context.Context, c execx.Command) (execx.Result, error) {
	r.commands = append(r.commands, c)
	v := r.results[0]
	r.results = r.results[1:]
	return v, r.err
}
func TestSudoPlatformStrictFixedProbe(t *testing.T) {
	fresh := func() *sudoProbe {
		return &sudoProbe{results: []execx.Result{{Stdout: "27.0.1\n", StderrComplete: true}, {Stdout: "26A434\n", StderrComplete: true}}}
	}
	r := fresh()
	if sudoPlatform(r, "darwin", "arm64") != nil {
		t.Fatal("exact trial platform refused")
	}
	want := []execx.Command{{Path: "/usr/bin/sw_vers", Args: []string{"-productVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}, Stdin: []byte{}}, {Path: "/usr/bin/sw_vers", Args: []string{"-buildVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}, Stdin: []byte{}}}
	if !reflect.DeepEqual(r.commands, want) {
		t.Fatal("probe argv/env/stdin changed", r.commands)
	}
	for _, name := range []string{"version", "build", "stderr", "drain", "truncated", "nonzero", "os", "arch"} {
		t.Run(name, func(t *testing.T) {
			r := fresh()
			goos, arch := "darwin", "arm64"
			switch name {
			case "version":
				r.results[0].Stdout = "26.6.2\n"
			case "build":
				r.results[1].Stdout = "25G83\n"
			case "stderr":
				r.results[0].Stderr = "unknown"
			case "drain":
				r.results[0].StderrComplete = false
			case "truncated":
				r.results[0].Truncated = true
			case "nonzero":
				r.err = errors.New("exit1")
			case "os":
				goos = "linux"
			case "arch":
				arch = "amd64"
			}
			if sudoPlatform(r, goos, arch) == nil {
				t.Fatal("unknown platform admitted")
			}
		})
	}
}
