package workspacex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportSnapshotDistinctnessUsesPinnedCurrentSourceIdentity(t *testing.T) {
	root := privateRoot(t)
	path := filepath.Join(root, "source.raw")
	if err := os.WriteFile(path, []byte("source"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		t.Fatal(err)
	}
	current, err := diskIdentity(info)
	if err != nil {
		t.Fatal(err)
	}
	historical := current
	historical.Device++ // a remount can make the durable record's device stale
	if historical == current {
		t.Fatal("invalid synthetic device drift")
	}
	if err := requireDistinctExportSnapshot(current, source); err == nil {
		t.Fatal("snapshot aliased the pinned current source despite historical drift")
	}
	other := filepath.Join(root, "snapshot.raw")
	if err := os.WriteFile(other, []byte("copy"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherInfo, err := os.Stat(other)
	if err != nil {
		t.Fatal(err)
	}
	copyID, err := diskIdentity(otherInfo)
	if err != nil {
		t.Fatal(err)
	}
	if err := requireDistinctExportSnapshot(copyID, source); err != nil {
		t.Fatal(err)
	}
}
