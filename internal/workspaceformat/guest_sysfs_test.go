package workspaceformat

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLinuxHasPartitionsSkipsRegularSysfsAttributes(t *testing.T) {
	sysPath := t.TempDir()
	for _, name := range []string{"size", "dev", "alignment_offset"} {
		if err := os.WriteFile(filepath.Join(sysPath, name), []byte("1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(sysPath, "holders"), 0o700); err != nil {
		t.Fatal(err)
	}
	partitioned, err := linuxHasPartitions(sysPath)
	if err != nil || partitioned {
		t.Fatalf("ordinary whole disk attributes = partitioned %v, err %v", partitioned, err)
	}
	child := filepath.Join(sysPath, "vdb1")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "partition"), []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	partitioned, err = linuxHasPartitions(sysPath)
	if err != nil || !partitioned {
		t.Fatalf("partition child = partitioned %v, err %v", partitioned, err)
	}
}
