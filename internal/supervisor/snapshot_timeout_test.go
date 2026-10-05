package supervisor

import (
	"context"
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"
	"time"
)

type delayedSnapshotRuntime struct {
	runtimeFixture
	delay    time.Duration
	canceled chan struct{}
}

func (o *delayedSnapshotRuntime) Snapshot(ctx context.Context) Snapshot {
	timer := time.NewTimer(o.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		close(o.canceled)
	}
	// Even a misbehaving observer's positive result after cancellation must
	// be cleared by the control handler before it becomes readiness evidence.
	return Snapshot{Binding: o.binding, BackendRunning: true, SerialHealthy: true,
		PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true}
}

func TestSnapshotBudgetAllowsSlowerReadinessAndPreservesCallerExpiry(t *testing.T) {
	for _, test := range []struct {
		name   string
		caller time.Duration
		ready  bool
	}{
		{name: "valid observation beyond old RPC budget", ready: true},
		{name: "short caller expiry clears late positive", caller: 160 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			lock, err := acquireGenerationLock(request)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			listener, err := listenSocket(filepath.Join(request.RuntimeDirectory, socketName))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			owner := &delayedSnapshotRuntime{runtimeFixture: runtimeFixture{binding: request.Binding},
				delay: 2200 * time.Millisecond, canceled: make(chan struct{})}
			served := make(chan struct{})
			go func() {
				defer close(served)
				connection, err := listener.AcceptUnix()
				if err == nil {
					handleControl(context.Background(), connection, request.Binding, owner, func() error { return nil })
				}
			}()
			ctx := context.Background()
			if test.caller != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, test.caller)
				defer cancel()
			}
			client := &Client{RuntimeDirectory: request.RuntimeDirectory}
			snapshot, err := client.Snapshot(ctx, request.Binding)
			if err != nil {
				t.Fatal(err)
			}
			<-served
			if snapshot.Binding != request.Binding || snapshotReady(snapshot) != test.ready {
				t.Fatalf("snapshot = %+v, want exact binding and ready=%t", snapshot, test.ready)
			}
			select {
			case <-owner.canceled:
				if test.ready || snapshot.Diagnostic != "snapshot observation expired" ||
					snapshot.BackendRunning || snapshot.SerialHealthy || snapshot.PinPresent ||
					snapshot.CertificateCurrent || snapshot.ProbeOK || snapshot.ZoneMatches {
					t.Fatalf("canceled observation retained positive evidence: %+v", snapshot)
				}
			default:
				if !test.ready {
					t.Fatal("over-budget observer was not canceled")
				}
			}
		})
	}
}

func TestControlInitialFrameStillExpiresWithinTwoSeconds(t *testing.T) {
	binding := minimalRequest(t).Binding
	owner := &runtimeFixture{}
	server, client := net.Pipe()
	defer client.Close()
	served := make(chan struct{})
	started := time.Now()
	go func() {
		handleControl(context.Background(), server, binding, owner, func() error { return nil })
		close(served)
	}()
	if err := client.SetDeadline(started.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], 32)
	if _, err := client.Write(header[:]); err != nil {
		t.Fatal(err)
	}
	// Hold the declared body indefinitely. Action-specific RPC budgets must
	// only apply after the complete bounded request has been admitted.
	select {
	case <-served:
		if elapsed := time.Since(started); elapsed < 1800*time.Millisecond || elapsed > 2700*time.Millisecond {
			t.Fatalf("partial frame ended after %s, want the 2s frame cap", elapsed)
		}
	case <-time.After(2700 * time.Millisecond):
		client.Close()
		<-served
		t.Fatal("partial frame outlived the 2s frame cap")
	}
	if owner.snapshots.Load() != 0 {
		t.Fatal("partial frame reached the snapshot observer")
	}
}
