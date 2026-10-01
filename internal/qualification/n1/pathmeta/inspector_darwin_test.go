//go:build darwin

package pathmeta

import (
	"errors"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestNativeEmptyAndPopulatedPrivateACL(t *testing.T) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "acl-fixture")
	if os.WriteFile(path, []byte("nonsecret"), 0600) != nil {
		t.Fatal("writefixture")
	}
	i := OSInspector{}
	has, e := i.HasExtendedACL(path)
	if e != nil || has {
		t.Fatal("empty private ACL refused", has, e)
	}
	principal, e := aclFixturePrincipal(os.Getuid(), user.LookupId)
	if e != nil {
		t.Fatal("current fixture account lookup", e)
	}
	info, e := os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() || int(st.Gid) != os.Getgid() || st.Nlink != 1 || info.Mode() != 0600 {
		t.Fatal("private fixture metadata", info)
	}
	c := exec.Command("/bin/chmod", "+a", principal+" allow read", path)
	c.Env = []string{"LC_ALL=C", "LANG=C"}
	if c.Run() != nil {
		t.Fatal("private ACL fixture creation failed")
	}
	has, e = i.HasExtendedACL(path)
	if e != nil || !has {
		t.Fatal("populated ACL falsely admitted", has, e)
	}
	info, e = os.Lstat(path)
	if e != nil {
		t.Fatal(e)
	}
	if Check(path, info, i) == nil {
		t.Fatal("populated private ACL accepted")
	}
}

func aclFixturePrincipal(uid int, lookup func(string) (*user.User, error)) (string, error) {
	id := strconv.Itoa(uid)
	u, e := lookup(id)
	if e != nil || u == nil || u.Uid != id || u.Username == "" {
		return "", errors.New("fixture account refused")
	}
	return "user:" + u.Username, nil
}
func TestACLFixtureUsesCurrentAccount(t *testing.T) {
	got, e := aclFixturePrincipal(1001, func(id string) (*user.User, error) {
		if id != "1001" {
			t.Fatal("lookup uid", id)
		}
		return &user.User{Uid: id, Username: "runner"}, nil
	})
	if e != nil || got != "user:runner" {
		t.Fatal("fixture selected foreign account", got, e)
	}
	for _, mode := range []string{"lookup-error", "nil", "foreign-uid", "empty-name"} {
		t.Run(mode, func(t *testing.T) {
			_, e := aclFixturePrincipal(1001, func(id string) (*user.User, error) {
				switch mode {
				case "lookup-error":
					return nil, errors.New("lookup")
				case "nil":
					return nil, nil
				case "foreign-uid":
					return &user.User{Uid: "501", Username: "devel"}, nil
				default:
					return &user.User{Uid: id}, nil
				}
			})
			if e == nil {
				t.Fatal("invalid fixture identity admitted")
			}
		})
	}
}
