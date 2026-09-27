//go:build darwin && cgo

package serialx

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateRuntimeRejectsExtendedGenerationACL(t *testing.T) {
	generation := privateGeneration(t)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("/bin/chmod", "+a", "user:"+account.Username+" allow read", generation)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("add ACL: %v: %s", err, output)
	}
	defer exec.Command("/bin/chmod", "-N", generation).Run()
	if _, err := CreateRuntime(context.Background(), generation); err == nil || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("extended generation ACL accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(generation, "serial")); !os.IsNotExist(err) {
		t.Fatalf("serial subtree created: %v", err)
	}
}

type slaveACLFixture struct{}

func (slaveACLFixture) HasExtendedACL(path string) (bool, error) {
	return strings.HasPrefix(path, "/dev/"), nil
}

func TestCreateRuntimeRejectsExtendedSlaveACLAndCleansSubtree(t *testing.T) {
	previous := serialACLInspector
	serialACLInspector = slaveACLFixture{}
	t.Cleanup(func() { serialACLInspector = previous })
	generation := privateGeneration(t)
	if _, err := CreateRuntime(context.Background(), generation); err == nil || !strings.Contains(err.Error(), "ACL") {
		t.Fatalf("extended PTY ACL accepted: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(generation, "serial")); !os.IsNotExist(err) {
		t.Fatalf("serial subtree retained: %v", err)
	}
}
