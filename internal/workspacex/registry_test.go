package workspacex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestListSessionAttachmentsReturnsOnlyExactBoundedSession(t *testing.T) {
	root := privateRoot(t)
	first := fixtureRecord()
	first.State = StateAvailable
	first.Disk = &DiskIdentity{Device: 1, Inode: 2}
	first.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/first"}
	writeRawVolumeRecord(t, root, first.VolumeID, mustJSON(t, first))
	second := first
	second.VolumeID = replacementSessionID
	second.FilesystemUUID = "20335577-99bb-4cdd-8eff-0123456789ab"
	second.Disk = &DiskIdentity{Device: 1, Inode: 3}
	second.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/second"}
	writeRawVolumeRecord(t, root, second.VolumeID, mustJSON(t, second))
	other := first
	other.VolumeID = "10335577-99bb-4cdd-8eff-0123456789ab"
	other.Attachment = &Attachment{SessionID: replacementSessionID, SessionName: "next", MountPath: "/home/boxwarden/workspaces/first"}
	writeRawVolumeRecord(t, root, other.VolumeID, mustJSON(t, other))
	got, err := ListSessionAttachments(context.Background(), root, domain.ID("work"), testSessionID, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].VolumeID != first.VolumeID || got[1].VolumeID != second.VolumeID {
		t.Fatalf("exact attachments = %#v", got)
	}
	if _, err := ListSessionAttachments(context.Background(), root, domain.ID("work"), replacementSessionID, "dev"); err == nil {
		t.Fatal("stale session UUID accepted")
	}
}

func TestListSessionAttachmentsAllowsMissingRegistry(t *testing.T) {
	got, err := ListSessionAttachments(context.Background(), privateRoot(t), domain.ID("work"), testSessionID, "dev")
	if err != nil || len(got) != 0 {
		t.Fatalf("missing registry = %#v, %v", got, err)
	}
}

func TestListSessionAttachmentsRejectsCorruptRegistryAndOverLimit(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{"invalid name", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "workspaces", "bad.json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed other record", func(t *testing.T, root string) {
			if err := os.WriteFile(filepath.Join(root, "workspaces", replacementSessionID+".json"), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate filesystem UUID", func(t *testing.T, root string) {
			more := fixtureRecord()
			more.VolumeID = replacementSessionID
			more.State = StateAvailable
			more.Disk = &DiskIdentity{Device: 1, Inode: 3}
			more.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/second"}
			writeRawVolumeRecord(t, root, more.VolumeID, mustJSON(t, more))
		}},
		{"duplicate mount path", func(t *testing.T, root string) {
			more := fixtureRecord()
			more.VolumeID = replacementSessionID
			more.FilesystemUUID = "20335577-99bb-4cdd-8eff-0123456789ab"
			more.State = StateAvailable
			more.Disk = &DiskIdentity{Device: 1, Inode: 3}
			more.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/first"}
			writeRawVolumeRecord(t, root, more.VolumeID, mustJSON(t, more))
		}},
		{"fifth attachment", func(t *testing.T, root string) {
			for i := 1; i <= 4; i++ {
				more := fixtureRecord()
				more.VolumeID = "00000000-0000-4000-8000-00000000000" + string('0'+byte(i))
				more.FilesystemUUID = "10000000-0000-4000-8000-00000000000" + string('0'+byte(i))
				more.State = StateAvailable
				more.Disk = &DiskIdentity{Device: 1, Inode: uint64(i + 2)}
				more.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/extra" + string('0'+byte(i))}
				writeRawVolumeRecord(t, root, more.VolumeID, mustJSON(t, more))
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := privateRoot(t)
			one := fixtureRecord()
			one.State = StateAvailable
			one.Disk = &DiskIdentity{Device: 1, Inode: 2}
			one.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/first"}
			writeRawVolumeRecord(t, root, one.VolumeID, mustJSON(t, one))
			test.mutate(t, root)
			if _, err := ListSessionAttachments(context.Background(), root, domain.ID("work"), testSessionID, "dev"); err == nil {
				t.Fatal("unsafe registry accepted")
			} else if test.name == "fifth attachment" && !strings.Contains(err.Error(), "exceeds 4") {
				t.Fatalf("fifth attachment rejected for wrong reason: %v", err)
			}
		})
	}
}
