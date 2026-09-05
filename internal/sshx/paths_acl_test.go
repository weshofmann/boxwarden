package sshx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrivatePathAdmissionRejectsExtendedACLAndACLInspectionFailures(t *testing.T) {
	root := privateRoot(t)
	file := filepath.Join(root, "credential")
	mustWrite(t, file, []byte("credential"), 0o600)
	directoryInfo, err := os.Lstat(root)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	for name, inspector := range map[string]privateACLInspector{
		"extended ACL":     privateACLInspectorFake{extended: true},
		"inspection error": privateACLInspectorFake{err: errors.New("unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := requirePrivateDirectoryInfoWithACL(root, directoryInfo, inspector); err == nil {
				t.Fatal("private directory admission accepted an unsafe or unverifiable ACL")
			}
			if err := requireFileInfoWithACL(file, fileInfo, privateFileMode, inspector); err == nil {
				t.Fatal("private file admission accepted an unsafe or unverifiable ACL")
			}
		})
	}
}

type privateACLInspectorFake struct {
	extended bool
	err      error
}

func (i privateACLInspectorFake) HasExtendedACL(string) (bool, error) { return i.extended, i.err }
