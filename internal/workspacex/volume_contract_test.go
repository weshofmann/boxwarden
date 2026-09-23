package workspacex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/session"
)

const replacementSessionID = "7fb25db7-3cc1-4d92-a04c-b60fd05fa421"

type stoppedObserver struct {
	state  backend.ObjectState
	object string
	err    error
}

func (o stoppedObserver) Observe(_ context.Context, objectID string) (backend.Observation, error) {
	if o.err != nil {
		return backend.Observation{}, o.err
	}
	if o.object != objectID {
		return backend.Observation{ObjectID: o.object, Exists: true, State: o.state}, nil
	}
	return backend.Observation{ObjectID: objectID, Exists: true, State: o.state}, nil
}

func TestVolumeRecordSurvivesDetachAndAttachToAnotherSession(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.State = StateAvailable
	record.Disk = &DiskIdentity{Device: 1, Inode: 2}
	writeRawRecord(t, root, mustJSON(t, record))
	if _, err := os.Stat(filepath.Join(root, "workspaces", testVolumeID+".json")); err != nil {
		t.Fatalf("volume-keyed record missing: %v", err)
	}
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	writeStoppedSession(t, root, "next", replacementSessionID, "bw-work-next")
	first, err := Attach(context.Background(), root, domain.ID("work"), testVolumeID, "dev", "/home/boxwarden/workspaces/project", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
	if err != nil {
		t.Fatalf("Attach A: %v", err)
	}
	if first.Attachment == nil || first.Attachment.SessionID != testSessionID {
		t.Fatalf("A attachment = %#v", first.Attachment)
	}
	if _, err := Attach(context.Background(), root, domain.ID("work"), testVolumeID, "next", "/home/boxwarden/workspaces/project", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-next"}); err == nil {
		t.Fatal("attached to B while still owned by A")
	}
	unattached, err := Detach(context.Background(), root, domain.ID("work"), testVolumeID, "dev", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
	if err != nil {
		t.Fatalf("Detach A: %v", err)
	}
	if unattached.Attachment != nil {
		t.Fatalf("detached record still attached: %#v", unattached.Attachment)
	}
	second, err := Attach(context.Background(), root, domain.ID("work"), testVolumeID, "next", "/home/boxwarden/workspaces/project", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-next"})
	if err != nil {
		t.Fatalf("Attach B: %v", err)
	}
	if second.VolumeID != testVolumeID || second.Attachment == nil || second.Attachment.SessionID != replacementSessionID {
		t.Fatalf("B attachment = %#v", second)
	}
}

func TestAttachAndDetachRejectUncertainBackendState(t *testing.T) {
	for label, observer := range map[string]stoppedObserver{
		"running":  {state: backend.ObjectRunning, object: "bw-work-dev"},
		"unknown":  {state: backend.ObjectUnknown, object: "bw-work-dev"},
		"mismatch": {state: backend.ObjectStopped, object: "bw-work-other"},
		"error":    {err: errors.New("list failed")},
	} {
		t.Run(label, func(t *testing.T) {
			root := privateRoot(t)
			record := fixtureRecord()
			record.State = StateAvailable
			record.Disk = &DiskIdentity{Device: 1, Inode: 2}
			writeRawRecord(t, root, mustJSON(t, record))
			writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
			if _, err := Attach(context.Background(), root, domain.ID("work"), testVolumeID, "dev", "/home/boxwarden/workspaces/project", observer); err == nil {
				t.Fatal("uncertain backend accepted")
			}
			loaded, err := LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			if loaded.Attachment != nil {
				t.Fatalf("failed attach persisted: %#v", loaded.Attachment)
			}
			attached, err := Attach(context.Background(), root, domain.ID("work"), testVolumeID, "dev", "/home/boxwarden/workspaces/project", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Detach(context.Background(), root, domain.ID("work"), testVolumeID, "dev", observer); err == nil {
				t.Fatal("uncertain detach accepted")
			}
			persisted, err := LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Attachment == nil || persisted.Attachment.SessionID != attached.Attachment.SessionID {
				t.Fatalf("failed detach changed binding: %#v", persisted.Attachment)
			}
		})
	}
}

func TestReleaseUseRequiresExactObservedStoppedBackend(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.State = StateAvailable
	record.Disk = &DiskIdentity{Device: 1, Inode: 2}
	record.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project"}
	record.Use = &Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}
	writeRawRecord(t, root, mustJSON(t, record))
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	wrong := *record.Use
	wrong.Generation = replacementSessionID
	if _, err := ReleaseUseAfterObservedStop(context.Background(), root, domain.ID("work"), testVolumeID, wrong, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}); err == nil {
		t.Fatal("wrong generation cleared reservation")
	}
	if _, err := ReleaseUseAfterObservedStop(context.Background(), root, domain.ID("work"), testVolumeID, *record.Use, stoppedObserver{state: backend.ObjectRunning, object: "bw-work-dev"}); err == nil {
		t.Fatal("running backend cleared reservation")
	}
	if _, err := ReleaseUseAfterObservedStop(context.Background(), root, domain.ID("work"), testVolumeID, *record.Use, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-other"}); err == nil {
		t.Fatal("mismatched observation cleared reservation")
	}
	loaded, err := LoadRecord(root, domain.ID("work"), testVolumeID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Use == nil {
		t.Fatal("reservation lost after refused release")
	}
	cleared, err := ReleaseUseAfterObservedStop(context.Background(), root, domain.ID("work"), testVolumeID, *record.Use, stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"})
	if err != nil {
		t.Fatalf("release after exact stop: %v", err)
	}
	if cleared.Use != nil || cleared.Attachment == nil {
		t.Fatalf("release changed wrong fields: %#v", cleared)
	}
}

func TestReserveUseRequiresExactStoppedAttachedSession(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.State = StateAvailable
	record.Disk = &DiskIdentity{Device: 1, Inode: 2}
	record.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project"}
	writeRawRecord(t, root, mustJSON(t, record))
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	want := Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}
	if _, err := ReserveUse(context.Background(), root, domain.ID("work"), testVolumeID, want, stoppedObserver{state: backend.ObjectRunning, object: want.BackendObject}); err == nil {
		t.Fatal("running backend gained a use reservation")
	}
	wrong := want
	wrong.BackendObject = "bw-work-other"
	if _, err := ReserveUse(context.Background(), root, domain.ID("work"), testVolumeID, wrong, stoppedObserver{state: backend.ObjectStopped, object: wrong.BackendObject}); err == nil {
		t.Fatal("wrong session backend gained a use reservation")
	}
	reserved, err := ReserveUse(context.Background(), root, domain.ID("work"), testVolumeID, want, stoppedObserver{state: backend.ObjectStopped, object: want.BackendObject})
	if err != nil {
		t.Fatalf("ReserveUse: %v", err)
	}
	if reserved.Use == nil || *reserved.Use != want {
		t.Fatalf("durable use = %#v, want %#v", reserved.Use, want)
	}
	if _, err := ReserveUse(context.Background(), root, domain.ID("work"), testVolumeID, want, stoppedObserver{state: backend.ObjectStopped, object: want.BackendObject}); err == nil {
		t.Fatal("duplicate use reservation accepted")
	}
}

func TestAdmitDiskFailsClosedWithoutQualifiedCreationAndExt4Proof(t *testing.T) {
	root := privateRoot(t)
	record := fixtureRecord()
	record.State = StateAvailable
	record.Disk = &DiskIdentity{Device: 1, Inode: 2}
	record.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project"}
	record.Use = &Use{BackendKind: "tart", BackendObject: "bw-work-dev", Generation: testGeneration}
	writeRawRecord(t, root, mustJSON(t, record))
	if _, err := AdmitDisk(root, record); err == nil {
		t.Fatal("unqualified disk admitted for launch")
	}
}

func TestGenericSaveCannotPromoteUnverifiedDiskOrClearPendingOperation(t *testing.T) {
	root := privateRoot(t)
	creating := fixtureRecord()
	if err := SaveRecord(root, domain.ID("work"), creating); err != nil {
		t.Fatal(err)
	}
	unverified := creating
	unverified.State = StateAvailable
	unverified.Disk = &DiskIdentity{Device: 1, Inode: 2}
	if err := SaveRecord(root, domain.ID("work"), unverified); err == nil {
		t.Fatal("unverified file promoted to available")
	}
	pending := creating
	pending.Pending = &Pending{Kind: "format", ID: replacementSessionID}
	if err := SaveRecord(root, domain.ID("work"), pending); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecord(root, domain.ID("work"), creating); err == nil {
		t.Fatal("pending operation silently cleared")
	}
}

func TestAttachRejectsOverlappingMountForSameSandbox(t *testing.T) {
	root := privateRoot(t)
	first := fixtureRecord()
	first.State = StateAvailable
	first.Disk = &DiskIdentity{Device: 1, Inode: 2}
	first.Attachment = &Attachment{SessionID: testSessionID, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project"}
	writeRawRecord(t, root, mustJSON(t, first))
	second := fixtureRecord()
	second.VolumeID = replacementSessionID
	second.State = StateAvailable
	second.Disk = &DiskIdentity{Device: 3, Inode: 4}
	writeRawVolumeRecord(t, root, second.VolumeID, mustJSON(t, second))
	writeStoppedSession(t, root, "dev", testSessionID, "bw-work-dev")
	if _, err := Attach(context.Background(), root, domain.ID("work"), second.VolumeID, "dev", "/home/boxwarden/workspaces/project", stoppedObserver{state: backend.ObjectStopped, object: "bw-work-dev"}); err == nil {
		t.Fatal("overlapping mount accepted")
	}
}

func writeStoppedSession(t *testing.T, root, name, id, object string) {
	t.Helper()
	parsed, err := session.ParseName(name)
	if err != nil {
		t.Fatal(err)
	}
	record := session.Record{Version: 2, Domain: domain.ID("work"), Name: parsed, ID: id, Mode: session.ModeClean, IntendedState: session.StateStopped, Backend: session.BackendRef{Kind: "tart", ObjectID: object}, GoldenRevision: "golden-r1", Readiness: session.ReadinessRecord{Status: session.ReadinessNotReady}}
	if err := session.SaveRecord(root, domain.ID("work"), record); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func writeRawVolumeRecord(t *testing.T, root, volumeID, raw string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "workspaces"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspaces", volumeID+".json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}
