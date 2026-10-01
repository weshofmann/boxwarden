package fixed

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"strings"
	"testing"
)

func TestStaticReadDoesNotDependOnRetiredOrFutureRuntimeInputs(t *testing.T) {
	reads := []string{}
	_, e := readStatic(func(p string, cap, uid int, mode uint32, gid int) ([]byte, error) {
		reads = append(reads, p)
		if p != contract.LockPath || cap != contract.MaxLockBytes || uid != 501 || mode != 0600 || gid != -1 {
			t.Fatal("unexpected static input")
		}
		return []byte(`{"version":1}`), nil
	}, func(p string) error {
		if strings.Contains(p, ".enrolled.json") || strings.Contains(p, "live-window") {
			t.Fatal("future input")
		}
		return nil
	})
	if e == nil || len(reads) != 1 {
		t.Fatal("legacy lock admitted")
	}
	for i := 0; i < 2; i++ {
		expected := contract.StaticFilePath(13 + i)
		target := contract.EnrolledTarget(i)
		if expected == target || !strings.Contains(expected, "/config-expected/") || !strings.Contains(target, "/config/") {
			t.Fatal("precreated enrollment alias")
		}
	}
}
