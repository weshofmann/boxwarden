//go:build darwin && n1diagnostic && n1cleanup && !n1candidate

package pathmeta

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

type ancestorInfo struct{ x contract.ImageMetadata }

func (f ancestorInfo) Name() string       { return f.x.Path }
func (f ancestorInfo) Size() int64        { return int64(f.x.Bytes) }
func (f ancestorInfo) Mode() os.FileMode  { return os.ModeDir | os.FileMode(f.x.Mode) }
func (f ancestorInfo) ModTime() time.Time { return time.Unix(0, int64(f.x.MtimeNS)) }
func (f ancestorInfo) IsDir() bool        { return true }
func (f ancestorInfo) Sys() any {
	return &syscall.Stat_t{Dev: int32(f.x.Device), Ino: f.x.Inode, Uid: f.x.UID, Gid: f.x.GID, Nlink: uint16(f.x.Nlink), Mtimespec: syscall.Timespec{Sec: int64(f.x.MtimeNS / 1e9), Nsec: int64(f.x.MtimeNS % 1e9)}, Ctimespec: syscall.Timespec{Sec: int64(f.x.CtimeNS / 1e9), Nsec: int64(f.x.CtimeNS % 1e9)}, Flags: f.x.Flags}
}
func actualAncestors(t *testing.T) []contract.ImageMetadata {
	t.Helper()
	raw, e := os.ReadFile("../contract/testdata/protected-ancestry.json")
	if e != nil || contract.SHA(raw) != "ccb9ea8a575605aa53e9b8727bfa9d88bf475f116c8bbe8fde25006352fb4652" {
		t.Fatal("actual ancestor proof", e)
	}
	var p struct{ Records []contract.ImageMetadata }
	if json.Unmarshal(raw, &p) != nil {
		t.Fatal("proof parse")
	}
	return p.Records
}
func aclOutput(p string) string {
	mode := "drwx------+"
	if p == "/Users/devel" {
		mode = "drwxr-x---+"
	}
	return mode + " 46 devel staff 1472 Sep 30 00:00 " + p + "\n 0: group:everyone deny delete\n"
}
func TestExactAncestorACLRejectsAdditionalAuthority(t *testing.T) {
	for _, x := range actualAncestors(t) {
		if contract.ProtectedAncestorMode(x.Path) == 0 {
			continue
		}
		s := aclOutput(x.Path)
		if !exactDenyDeleteOutput(x.Path, s) {
			t.Fatal("known deny-delete refused")
		}
		for _, bad := range []string{strings.Replace(s, "deny delete", "allow delete", 1), strings.Replace(s, "deny delete", "deny write", 1), strings.Replace(s, "deny delete", "deny delete,file_inherit", 1), strings.Replace(s, " 0:", " 1:", 1), s + " 1: user:devel allow write\n", strings.Replace(s, "group:everyone", "user:devel", 1), strings.TrimSuffix(s, "\n"), strings.Replace(s, x.Path, x.Path+"/foreign", 1)} {
			if exactDenyDeleteOutput(x.Path, bad) {
				t.Fatal("foreign/additional ACL admitted", bad)
			}
		}
		if exactDenyDeleteOutput(x.Path+"/foreign", s) {
			t.Fatal("prefix exception")
		}
	}
}
func TestAncestorFullMetadataAndACLBrackets(t *testing.T) {
	for _, x := range actualAncestors(t) {
		if contract.ProtectedAncestorMode(x.Path) == 0 {
			continue
		}
		for _, mode := range []string{"positive", "uid", "gid", "mode", "inode", "device", "nlink", "size", "mtime", "ctime", "flags", "canonical", "platform", "acl", "late-acl", "stat"} {
			t.Run(x.Path+mode, func(t *testing.T) {
				stats, acls := 0, 0
				i := ancestorChecks{stat: func(string) (os.FileInfo, error) {
					stats++
					y := x
					if stats >= 2 {
						switch mode {
						case "uid":
							y.UID++
						case "gid":
							y.GID++
						case "mode":
							y.Mode = 0770
						case "inode":
							y.Inode++
						case "device":
							y.Device++
						case "nlink":
							y.Nlink++
						case "size":
							y.Bytes++
						case "mtime":
							y.MtimeNS++
						case "ctime":
							y.CtimeNS++
						case "flags":
							y.Flags++
						case "stat":
							return nil, contract.ErrRefused
						}
					}
					return ancestorInfo{y}, nil
				}, canonical: func(p string) (string, error) {
					if mode == "canonical" {
						return p + "/foreign", nil
					}
					return p, nil
				}, acl: func(string) (bool, error) { acls++; return !(mode == "acl" || mode == "late-acl" && acls == 2), nil }, platform: func() error {
					if mode == "platform" {
						return contract.ErrRefused
					}
					return nil
				}}
				e := checkQualificationAncestor(x.Path, ancestorInfo{x}, i)
				if mode == "positive" {
					if e != nil || stats != 3 || acls != 2 {
						t.Fatal(e, stats, acls)
					}
				} else if e == nil {
					t.Fatal("drift admitted")
				}
			})
		}
	}
}

type ancestorRunner struct {
	t    *testing.T
	p    string
	mode string
}

func (r ancestorRunner) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	r.t.Helper()
	if c.Path != "/bin/ls" || !reflect.DeepEqual(c.Args, []string{"-lde", r.p}) || !reflect.DeepEqual(c.Env, []string{"LC_ALL=C", "LANG=C"}) || len(c.Stdin) != 0 {
		r.t.Fatal("ACL command contract", c)
	}
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 5*time.Second {
		r.t.Fatal("unbounded ACL query")
	}
	out := execx.Result{Stdout: aclOutput(r.p), StderrComplete: true}
	switch r.mode {
	case "error":
		return out, contract.ErrRefused
	case "truncated":
		out.Truncated = true
	case "stdout-truncated":
		out.StdoutTruncated = true
	case "stderr-truncated":
		out.StderrTruncated = true
	case "stderr":
		out.Stderr = "unknown"
	case "lost-eof":
		out.StderrComplete = false
	}
	return out, nil
}
func TestBoundedACLQueryContract(t *testing.T) {
	for _, mode := range []string{"positive", "error", "truncated", "stdout-truncated", "stderr-truncated", "stderr", "lost-eof"} {
		p := "/Users/devel"
		ok, e := denyDeleteACL(ancestorRunner{t, p, mode}, p)
		if mode == "positive" {
			if !ok || e != nil {
				t.Fatal(e)
			}
		} else if e == nil {
			t.Fatal("unknown output admitted", mode)
		}
	}
}
