//go:build darwin && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package closeout

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"strings"
	"testing"
)

func TestCandidateAbsenceRequiresEveryProtectedParentAndRetainedClosure(t *testing.T) {
	for _, mode := range []string{"positive", "present", "missing-parent", "permission", "owner", "group", "mode", "acl", "canonical", "descriptor", "close", "late-parent"} {
		t.Run(mode, func(t *testing.T) {
			closed, opened, stats, diskReads := 0, 0, 0, 0
			lookup := func(p string) imageInfo {
				return imageInfo{contract.ImageMetadata{Path: p, Device: 7, Inode: 13, UID: 0, GID: 0, Mode: 0755, Nlink: 2, Bytes: 1, MtimeNS: 1, CtimeNS: 1}, true}
			}
			i := protectedInspector{
				stat: func(p string) (os.FileInfo, error) {
					stats++
					if p == candidateTree {
						if mode == "permission" {
							return nil, os.ErrPermission
						}
						if mode == "present" {
							return lookup(p), nil
						}
						return nil, os.ErrNotExist
					}
					if mode == "missing-parent" && p == candidateParents[4] {
						return nil, os.ErrNotExist
					}
					f := lookup(p)
					if mode == "owner" {
						f.x.UID = 501
					}
					if mode == "group" {
						f.x.GID = 20
					}
					if mode == "mode" {
						f.x.Mode = 0777
					}
					if mode == "late-parent" && stats > len(candidateParents) {
						f.x.CtimeNS++
					}
					return f, nil
				},
				canonical: func(p string) (string, error) {
					if mode == "canonical" {
						return p + "/foreign", nil
					}
					return p, nil
				},
				acl: func(string, os.FileInfo, string) error {
					if mode == "acl" {
						return ErrRefused
					}
					return nil
				},
				open: func(p string, dir bool) (protectedHandle, error) {
					if !dir || p == candidateTree {
						t.Fatal("candidate leaf opened")
					}
					opened++
					return &protectedFakeHandle{info: lookup(p), reader: strings.NewReader(""), closed: &closed, diskReads: &diskReads, mode: mode}, nil
				},
			}
			e := candidateAbsent(t.Context(), i)
			if (mode == "positive") != (e == nil) {
				t.Fatal(mode, e)
			}
			if opened != closed {
				t.Fatal("unchecked retained handles", opened, closed)
			}
		})
	}
}
