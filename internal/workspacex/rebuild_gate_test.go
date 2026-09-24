package workspacex

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/session"
)

func TestStoppedRebuildGateHoldsVolumeSessionAndStorageThroughReservation(t *testing.T) {
	root, stopped := stoppedLaunchFixture(t)
	observer := stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}
	called := false
	err := WithStoppedRebuildGate(context.Background(), root, domain.ID("work"), stopped, observer, func() error {
		called = true
		for name, acquire := range map[string]func(context.Context) error{
			"volume": func(ctx context.Context) error {
				held, err := AcquireVolumeUse(ctx, root, domain.ID("work"), testVolumeID)
				if err == nil {
					_ = held.Release()
				}
				return err
			},
			"session": func(ctx context.Context) error {
				held, err := lock.AcquireSession(ctx, root, "work", "dev")
				if err == nil {
					_ = held.Release()
				}
				return err
			},
			"storage": func(ctx context.Context) error {
				held, err := AcquireStorageOperation(ctx, root, domain.ID("work"))
				if err == nil {
					_ = held.Release()
				}
				return err
			},
		} {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			err := acquire(ctx)
			cancel()
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("%s lock was not retained during reservation: %v", name, err)
			}
		}
		return nil
	})
	if err != nil || !called {
		t.Fatalf("rebuild gate = called %t, error %v", called, err)
	}
}

func TestStoppedRebuildGateRejectsUsePendingAndChangedSession(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, string, session.Record)
	}{
		{name: "use", mutate: func(t *testing.T, root string, stopped session.Record) {
			volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			volume.Use = &Use{BackendKind: "tart", BackendObject: stopped.Backend.ObjectID, Generation: testGeneration}
			if err := saveRecordTransition(root, domain.ID("work"), volume, mutationReserveUse, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "pending", mutate: func(t *testing.T, root string, _ session.Record) {
			volume, err := LoadRecord(root, domain.ID("work"), testVolumeID)
			if err != nil {
				t.Fatal(err)
			}
			volume.Pending = &Pending{Kind: "export-snapshot", ID: "00112233-4455-4677-8899-aabbccddeeff"}
			if err := saveRecordTransition(root, domain.ID("work"), volume, mutationBeginExportSnapshot, nil); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "changed session", mutate: func(t *testing.T, root string, stopped session.Record) {
			stopped.GoldenRevision = "another-revision"
			if err := session.SaveRecord(root, domain.ID("work"), stopped); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, stopped := stoppedLaunchFixture(t)
			test.mutate(t, root, stopped)
			called := false
			err := WithStoppedRebuildGate(context.Background(), root, domain.ID("work"), stopped, stoppedObserver{state: backend.ObjectStopped, object: stopped.Backend.ObjectID}, func() error { called = true; return nil })
			if err == nil || called {
				t.Fatalf("unsafe rebuild reservation = called %t, error %v", called, err)
			}
		})
	}
}
