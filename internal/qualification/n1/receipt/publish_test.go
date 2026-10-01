package receipt

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type emptyACL struct{}

func (emptyACL) HasExtendedACL(string) (bool, error) { return false, nil }
func publicationFixture(t *testing.T) (publisher, contract.Completion) {
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(dir, 0700)
	w := contract.Window{LockSHA: strings.Repeat("a", 64), ID: "11111111-1111-1111-1111-111111111111", StartedUnixNS: 10, ExpiresUnixNS: 10 + contract.WindowNS, ContinuousStartNS: 20, ContinuousLimitNS: 20 + contract.WindowNS}
	c := contract.Completion{Version: 1, Window: w, HandoffSHA: strings.Repeat("b", 64), ArchiveSHA: strings.Repeat("c", 64), SoftnetSHA: contract.SoftnetSHA, Removed: 3, DirectoryRemoved: true, ParentSynced: true, HandlesClosed: true}
	return publisher{dir: dir, uid: os.Getuid(), gid: os.Getgid(), acl: emptyACL{}}, c
}
func TestPublicationActualPrivateMetadataReadbackAndNoOverwrite(t *testing.T) {
	p, c := publicationFixture(t)
	if e := p.publish(c); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(p.dir, contract.CompletionName)
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	info, e := os.Lstat(path)
	if e != nil || info.Mode() != 0600 {
		t.Fatal("private receipt metadata", info, e)
	}
	if _, e = contract.ParseCompletion(before); e != nil {
		t.Fatal(e)
	}
	if p.publish(c) == nil {
		t.Fatal("receipt overwrite/adoption")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("receipt overwritten")
	}
}
func TestPublicationEveryEffectReturnUnknown(t *testing.T) {
	for _, stage := range []string{"before-create", "after-create", "after-chown", "after-chmod", "after-write", "after-file-sync", "after-readback", "after-parent-sync", "after-file-close", "after-parent-close", "after-root-close", "after-final-read"} {
		t.Run(stage, func(t *testing.T) {
			p, c := publicationFixture(t)
			p.fail = func(s string) error {
				if s == stage {
					return errors.New("return unknown")
				}
				return nil
			}
			if e := p.publish(c); e == nil {
				t.Fatal("publication return failure certified")
			}
			if stage != "before-create" {
				if _, e := os.Lstat(filepath.Join(p.dir, contract.CompletionName)); e != nil {
					t.Fatal("after-effect evidence lost", e)
				}
			}
		})
	}
}
