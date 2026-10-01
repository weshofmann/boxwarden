//go:build (darwin || linux) && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessImageSingleLinkAdmission(t *testing.T) {
	for _, mode := range []string{"one", "two", "after-open"} {
		t.Run(mode, func(t *testing.T) {
			dir, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(dir, "image")
			raw := []byte("private synthetic executable image bytes")
			if e = os.WriteFile(path, raw, 0500); e != nil {
				t.Fatal(e)
			}
			if mode == "two" {
				if e = os.Link(path, path+".link"); e != nil {
					t.Fatal(e)
				}
			}
			opens := 0
			image, e := readProcessImage(path, func(p string) (*os.File, error) {
				opens++
				f, e := openProcessImage(p)
				if e == nil && mode == "after-open" {
					if e = os.Link(p, p+".link"); e != nil {
						f.Close()
						return nil, e
					}
				}
				return f, e
			})
			if mode == "one" {
				if e != nil || image.sha != contract.SHA(raw) || opens != 1 {
					t.Fatal("real one-link image refused", image, e, opens)
				}
			} else {
				if e == nil {
					t.Fatal("hardlinked image admitted", mode, image)
				}
				if mode == "two" && opens != 0 {
					t.Fatal("stable two-link image opened before refusal")
				}
			}
		})
	}
}
