//go:build darwin && cgo

package basebuild

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestInstallerStagingClonesIndependentAPFSBytes(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	var filesystem []byte
	for _, character := range fs.Fstypename {
		if character == 0 {
			break
		}
		filesystem = append(filesystem, byte(character))
	}
	if string(filesystem) != "apfs" {
		t.Skip("APFS cloning is unavailable on this test volume")
	}
	source := filepath.Join(root, "installer.iso")
	target := filepath.Join(root, "source.iso")
	data := bytes.Repeat([]byte("boxwarden-clone-test"), 1<<18)
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Unix(946684800, 0)
	if err := os.Chtimes(source, old, old); err != nil {
		t.Fatal(err)
	}
	if err := stageSourceFile(context.Background(), source, target, 0400, 4<<30, true); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0400 || !info.ModTime().Equal(old) {
		t.Fatalf("APFS clone metadata: info=%v err=%v", info, err)
	}
	if err := os.WriteFile(source, []byte("changed source"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("staged ISO changed with its source: err=%v", err)
	}
}

func TestInstallerStagingCopiesImmutableSource(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	var filesystem []byte
	for _, character := range fs.Fstypename {
		if character == 0 {
			break
		}
		filesystem = append(filesystem, byte(character))
	}
	if string(filesystem) != "apfs" {
		t.Skip("APFS cloning is unavailable on this test volume")
	}
	source := filepath.Join(root, "locked-installer.iso")
	target := filepath.Join(root, "source.iso")
	data := []byte("pinned installer contents")
	if err := os.WriteFile(source, data, 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/chflags", "uchg", source).CombinedOutput(); err != nil {
		t.Fatalf("lock source: %v: %s", err, output)
	}
	t.Cleanup(func() {
		// A failing clone may have propagated uchg to the target.
		_ = exec.Command("/usr/bin/chflags", "nouchg", target).Run()
		if output, err := exec.Command("/usr/bin/chflags", "nouchg", source).CombinedOutput(); err != nil {
			t.Errorf("unlock source: %v: %s", err, output)
		}
	})
	if err := stageSourceFile(context.Background(), source, target, 0400, 4<<30, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("staged bytes: %q, %v", got, err)
	}
	info, err := os.Stat(target)
	if err != nil || info.Mode().Perm() != 0400 || info.Sys().(*syscall.Stat_t).Flags != 0 {
		t.Fatalf("staged metadata: %v, %v", info, err)
	}
}
