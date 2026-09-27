package basebuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTartBundleIdentityRejectsReplacementAndContentDrift(t *testing.T) {
	home := t.TempDir()
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(home, "vms", "candidate")
	if err := os.MkdirAll(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"config.json": "config", "disk.img": "disk", "nvram.bin": "nvram"} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := fingerprintTartBundle(context.Background(), home, "candidate")
	if err != nil {
		t.Fatal(err)
	}
	if !lowerHexDigest(first) {
		t.Fatalf("invalid identity %q", first)
	}
	if err := os.WriteFile(filepath.Join(bundle, "disk.img"), []byte("disk-changed"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := fingerprintTartBundle(context.Background(), home, "candidate")
	if err != nil || changed == first {
		t.Fatalf("disk content drift accepted: %q, %v", changed, err)
	}
	for _, name := range []string{"config.json", "disk.img", "nvram.bin"} {
		if err := os.Remove(filepath.Join(bundle, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(bundle); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"config.json": "config", "disk.img": "disk", "nvram.bin": "nvram"} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	replaced, err := fingerprintTartBundle(context.Background(), home, "candidate")
	if err != nil || replaced == first {
		t.Fatalf("same-name replacement accepted: %q, %v", replaced, err)
	}
}

func TestTartBundleIdentityRejectsUnknownLayoutAndSymlink(t *testing.T) {
	home := t.TempDir()
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(home, "vms", "candidate")
	if err := os.MkdirAll(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	for name := range map[string]bool{"config.json": true, "disk.img": true, "nvram.bin": true} {
		if err := os.WriteFile(filepath.Join(bundle, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(bundle, "unexpected"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintTartBundle(context.Background(), home, "candidate"); err == nil {
		t.Fatal("unknown bundle entry accepted")
	}
	if err := os.Remove(filepath.Join(bundle, "unexpected")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bundle, "nvram.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("config.json", filepath.Join(bundle, "nvram.bin")); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprintTartBundle(context.Background(), home, "candidate"); err == nil {
		t.Fatal("bundle symlink accepted")
	}
}
