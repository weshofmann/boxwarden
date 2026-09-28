package workspaceformat

import (
	"context"
	"testing"
)

// A clean remount can assign a new st_dev to the same host file. The existing
// formatter observation is retained here as history while the live file and
// ext4 header remain unchanged. A fresh v2 journal must bind the persistent
// APFS identity before this drift can be admitted.
func TestFreshVerifiedWorkspaceAdmitsAfterDeviceNumberChanges(t *testing.T) {
	root := testRoot(t)
	if _, err := Create(context.Background(), root, testRequest(), successfulFormatter(t)); err != nil {
		t.Fatal(err)
	}
	opened, err := openStateRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	volumes, err := openVolumes(opened, false)
	if err != nil {
		t.Fatal(err)
	}
	defer volumes.Close()
	journal, err := readJournal(volumes, testRequest())
	if err != nil {
		t.Fatal(err)
	}
	if journal.Version != 2 || journal.HostIdentity == nil {
		t.Fatalf("fresh formatter journal lacks durable APFS identity: %+v", journal)
	}
	journal.Identity.Device++ // Model a clean remount; inode and bytes stay fixed.
	if err := replaceJournal(volumes, journal); err != nil {
		t.Fatal(err)
	}
	file, _, err := Admit(root, testRequest())
	if err != nil {
		t.Fatalf("same formatted file rejected after simulated st_dev change: %v", err)
	}
	defer file.Close()
}
