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

func TestFixedPagedReaderUsesOnlyExactBoundedInputs(t *testing.T) {
	for _, mode := range []string{"positive", "index-error", "index-bytes", "rejection-error", "rejection-hash", "page-error", "page-hash", "page-bytes", "record-error", "record-hash", "record-bytes"} {
		t.Run(mode, func(t *testing.T) {
			s, in := fixedCatalogueFixture()
			values := map[string][]byte{contract.OSIndexPath: in.Index, contract.OSRejectionsPath: in.Rejections}
			caps := map[string]int{contract.OSIndexPath: 8192, contract.OSRejectionsPath: 65536}
			expected := []string{contract.OSIndexPath, contract.OSRejectionsPath}
			for i, b := range in.Pages {
				p := contract.SystemPagePath(i)
				values[p] = b
				caps[p] = 65536
				expected = append(expected, p)
			}
			for i, b := range in.Records {
				p := contract.QualificationPath(i)
				values[p] = b
				caps[p] = 8192
				expected = append(expected, p)
			}
			reads := []string{}
			c, e := readCatalogue(func(p string, cap, uid int, modeBits uint32, gid int) ([]byte, error) {
				if len(reads) >= len(expected) || p != expected[len(reads)] || cap != caps[p] || uid != 501 || modeBits != 0600 || gid != -1 {
					t.Fatal("unreviewed read", p, cap, uid, modeBits, gid)
				}
				reads = append(reads, p)
				b, ok := values[p]
				if !ok {
					t.Fatal("unknown path", p)
				}
				target := ""
				switch {
				case strings.HasPrefix(mode, "index-"):
					target = contract.OSIndexPath
				case strings.HasPrefix(mode, "rejection-"):
					target = contract.OSRejectionsPath
				case strings.HasPrefix(mode, "page-"):
					target = contract.SystemPagePath(0)
				case strings.HasPrefix(mode, "record-"):
					target = contract.QualificationPath(823)
				}
				if p == target {
					switch {
					case strings.HasSuffix(mode, "error"):
						return nil, ErrRefused
					case strings.HasSuffix(mode, "hash"):
						return append(append([]byte(nil), b...), ' '), nil
					case strings.HasSuffix(mode, "bytes"):
						return make([]byte, cap+1), nil
					}
				}
				return b, nil
			}, s)
			if mode == "positive" {
				if e != nil || !c.Valid() || len(reads) != 833 || reads[0] != contract.QualificationRoot+"/system-index.json" || reads[2] != contract.QualificationRoot+"/system-page-00.json" || reads[8] != contract.QualificationRoot+"/system-page-06.json" || reads[9] != contract.QualificationRoot+"/sudo.json" || reads[832] != contract.QualificationRoot+"/system-0822.json" {
					t.Fatal("finite read closure", e, len(reads))
				}
			} else if e == nil || c.Valid() {
				t.Fatal("invalid input became immutable catalogue", mode)
			}
			if strings.HasPrefix(mode, "index-") && len(reads) != 1 || strings.HasPrefix(mode, "rejection-") && len(reads) != 2 || strings.HasPrefix(mode, "page-") && len(reads) != 3 {
				t.Fatal("read continued after corrupt bounded layer", mode, len(reads))
			}
		})
	}
}
