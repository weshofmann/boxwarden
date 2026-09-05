//go:build darwin

package hostx

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

func TestManifestModeAllowsDistinctUnprivilegedUIDToRead(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to create root-owned files and drop the child credential")
	}

	nobody, err := user.Lookup("nobody")
	if err != nil {
		t.Fatal(err)
	}
	uid, err := strconv.ParseInt(nobody.Uid, 10, 64)
	if err != nil {
		t.Fatalf("parse nobody UID %q: %v", nobody.Uid, err)
	}
	gid, err := strconv.ParseInt(nobody.Gid, 10, 64)
	if err != nil {
		t.Fatalf("parse nobody GID %q: %v", nobody.Gid, err)
	}

	directory, err := os.MkdirTemp("/private/tmp", "boxwarden-manifest-permissions-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}

	privatePath := filepath.Join(directory, "private-manifest")
	if err := os.WriteFile(privatePath, []byte("private manifest control\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	if output, err := catAsCredential(privatePath, uid, gid); err == nil {
		t.Fatalf("cat private 0400 control succeeded with output %q", output)
	} else if !errors.As(err, new(*exec.ExitError)) {
		t.Fatalf("cat private 0400 control error = %v, want child access denial", err)
	}

	manifestPath := filepath.Join(directory, "manifest.json")
	want := []byte("non-secret manifest host metadata\n")
	if err := os.WriteFile(manifestPath, want, manifestMode); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != manifestMode {
		t.Fatalf("manifest mode = %04o, want %04o", got, manifestMode)
	}

	got, err := catAsCredential(manifestPath, uid, gid)
	if err != nil {
		t.Fatalf("cat manifest as nobody: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("manifest bytes = %q, want %q", got, want)
	}
}

func catAsCredential(path string, uid, gid int64) ([]byte, error) {
	cmd := exec.Command("/bin/cat", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid:         uint32(uid),
			Gid:         uint32(gid),
			NoSetGroups: true,
		},
	}
	return cmd.Output()
}
