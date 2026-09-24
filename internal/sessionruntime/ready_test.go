package sessionruntime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type readyAddress func(context.Context, string) (string, error)

func (f readyAddress) Resolve(ctx context.Context, object string) (string, error) {
	return f(ctx, object)
}

type readyIssuer func(context.Context, sshx.Binding, string, string) (sshx.Certificate, error)

func (f readyIssuer) Issue(ctx context.Context, binding sshx.Binding, root, key string) (sshx.Certificate, error) {
	return f(ctx, binding, root, key)
}

type readyClient struct {
	probe        func(sshx.Connection) error
	probeMounts  func(sshx.Connection, []sshx.WorkspaceMount) error
	ensureMounts func(sshx.Connection, []sshx.WorkspaceMount) error
	zone         string
	read         func(sshx.Connection) (string, error)
	apply        func(sshx.Connection, string) error
	inspect      func(sshx.Connection, []string) ([]sshx.PackageVersion, error)
	identity     func(sshx.Connection) (sshx.GuestIdentity, error)
}

func (c *readyClient) Probe(_ context.Context, connection sshx.Connection, request sshx.ProbeRequest) (sshx.ProbeResult, error) {
	if c.probe != nil {
		if err := c.probe(connection); err != nil {
			return sshx.ProbeResult{}, err
		}
	}
	if c.probeMounts != nil {
		if err := c.probeMounts(connection, request.Workspaces); err != nil {
			return sshx.ProbeResult{}, err
		}
	}
	return sshx.ProbeResult{OK: true}, nil
}
func (c *readyClient) EnsureWorkspaces(_ context.Context, connection sshx.Connection, mounts []sshx.WorkspaceMount) error {
	if c.ensureMounts != nil {
		return c.ensureMounts(connection, mounts)
	}
	return nil
}
func (c *readyClient) ApplyZone(_ context.Context, connection sshx.Connection, request sshx.ApplyZoneRequest) error {
	if c.apply != nil {
		return c.apply(connection, request.Zone)
	}
	c.zone = request.Zone
	return nil
}
func (c *readyClient) ReadZone(_ context.Context, connection sshx.Connection, _ sshx.ReadZoneRequest) (string, error) {
	if c.read != nil {
		return c.read(connection)
	}
	return c.zone, nil
}
func (c *readyClient) InspectPackages(_ context.Context, connection sshx.Connection, names []string) ([]sshx.PackageVersion, error) {
	if c.inspect == nil {
		return nil, errors.New("package inspection was not configured")
	}
	return c.inspect(connection, names)
}
func (c *readyClient) InspectIdentity(_ context.Context, connection sshx.Connection) (sshx.GuestIdentity, error) {
	if c.identity == nil {
		return sshx.GuestIdentity{}, errors.New("identity inspection was not configured")
	}
	return c.identity(connection)
}

func readyFixture(t *testing.T) (*fixture, *readyClient) {
	t.Helper()
	f := newFixture(t)
	client := &readyClient{}
	if err := os.MkdirAll(f.request.RuntimeDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	f.owner.deps.address = func(_, _ string) backend.AddressResolver {
		return readyAddress(func(_ context.Context, object string) (string, error) {
			if object != f.record.Backend.ObjectID {
				t.Errorf("resolved foreign backend %q", object)
			}
			return "192.0.2.10", nil
		})
	}
	f.owner.deps.key = func(_ context.Context, directory string) (string, error) {
		if directory != f.request.RuntimeDirectory {
			t.Errorf("key directory = %q", directory)
		}
		if err := os.WriteFile(filepath.Join(directory, "client"), []byte("key"), 0o600); err != nil {
			return "", err
		}
		return filepath.Join(directory, "client"), nil
	}
	f.owner.deps.issuer = func(_ sshx.CAIdentity) certificateIssuer {
		return readyIssuer(func(_ context.Context, binding sshx.Binding, root, key string) (sshx.Certificate, error) {
			if root != f.request.RuntimeDirectory || key != filepath.Join(root, "client") || binding.SessionID != f.record.ID {
				t.Errorf("issuer binding/root/key = %#v/%q/%q", binding, root, key)
			}
			if err := os.WriteFile(key+"-cert.pub", []byte("cert"), 0o644); err != nil {
				return sshx.Certificate{}, err
			}
			return sshx.Certificate{Path: key + "-cert.pub", Identity: binding.CertificateIdentity(), Principal: binding.Principal(), NotAfter: time.Now().Add(15 * time.Minute)}, nil
		})
	}
	f.owner.deps.client = client
	f.owner.deps.zone = func() (string, error) { return "America/Denver", nil }
	f.owner.deps.now = time.Now
	return f, client
}

func TestReadyConvergesCurrentGenerationAndSnapshotRechecksLiveEvidence(t *testing.T) {
	f, client := readyFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := f.owner.Snapshot(context.Background()); !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches {
		t.Fatalf("ready snapshot = %#v", snapshot)
	}
	client.probe = func(connection sshx.Connection) error { return errors.New("probe failed") }
	if snapshot := f.owner.Snapshot(context.Background()); snapshot.ProbeOK || snapshot.ZoneMatches || !strings.Contains(snapshot.Diagnostic, "probe") {
		t.Fatalf("snapshot reused old probe: %#v", snapshot)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestSnapshotRequiresRetainedChildAndFreshGuestProofWhenTartListsStopped(t *testing.T) {
	f, client := readyFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.observe = func(_ context.Context, object string) (backend.Observation, error) {
		return backend.Observation{ObjectID: object, Exists: true, State: backend.ObjectStopped}, nil
	}
	snapshot := f.owner.Snapshot(context.Background())
	if !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent || !snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches || !strings.Contains(snapshot.Diagnostic, "Tart listing") {
		t.Fatalf("false-stopped listing hid live exact owner: %#v", snapshot)
	}
	client.probe = func(sshx.Connection) error { return errors.New("probe failed") }
	if snapshot := f.owner.Snapshot(context.Background()); snapshot.ProbeOK || snapshot.ZoneMatches {
		t.Fatalf("false-stopped listing bypassed fresh probe: %#v", snapshot)
	}
	client.probe = func(sshx.Connection) error {
		_ = f.handle.Stop(context.Background())
		return nil
	}
	if snapshot := f.owner.Snapshot(context.Background()); snapshot.BackendRunning || snapshot.ProbeOK || snapshot.ZoneMatches {
		t.Fatalf("snapshot published READY after child exit during probe: %#v", snapshot)
	}
	_ = f.owner.Wait(context.Background())
}

func TestReadyRequiresWorkspaceMountAndFreshBoundProbe(t *testing.T) {
	f, client := readyFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	mount := sshx.WorkspaceMount{VolumeID: "00112233-4455-4677-8899-aabbccddeeff", FilesystemUUID: "10213243-5465-4768-899a-bbccddeeff00", MountPath: "/home/boxwarden/workspaces/project"}
	f.owner.mu.Lock()
	f.owner.workspaceMounts = []sshx.WorkspaceMount{mount}
	f.owner.mu.Unlock()
	ensures, boundProbes := 0, 0
	client.ensureMounts = func(_ sshx.Connection, mounts []sshx.WorkspaceMount) error {
		ensures++
		if len(mounts) != 1 || mounts[0] != mount {
			t.Fatalf("ensure binding = %+v", mounts)
		}
		return nil
	}
	client.probeMounts = func(_ sshx.Connection, mounts []sshx.WorkspaceMount) error {
		if len(mounts) > 0 {
			boundProbes++
			if len(mounts) != 1 || mounts[0] != mount {
				t.Fatalf("probe binding = %+v", mounts)
			}
		}
		return nil
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := f.owner.Snapshot(context.Background()); !snapshot.ProbeOK || !snapshot.ZoneMatches || ensures != 1 || boundProbes < 2 {
		t.Fatalf("mount-bound READY = %+v, ensures=%d probes=%d", snapshot, ensures, boundProbes)
	}
	client.probeMounts = func(_ sshx.Connection, mounts []sshx.WorkspaceMount) error {
		if len(mounts) > 0 {
			return errors.New("workspace unmounted")
		}
		return nil
	}
	if snapshot := f.owner.Snapshot(context.Background()); snapshot.ProbeOK || snapshot.ZoneMatches {
		t.Fatalf("lost workspace mount still READY: %+v", snapshot)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestOwnerPackageInspectionRequiresCurrentReadyGeneration(t *testing.T) {
	f, client := readyFixture(t)
	called := 0
	client.inspect = func(connection sshx.Connection, names []string) ([]sshx.PackageVersion, error) {
		called++
		if connection.Binding.SessionID != f.record.ID || len(names) != 1 || names[0] != "git" {
			t.Fatalf("inspection binding/names = %#v/%#v", connection.Binding, names)
		}
		return []sshx.PackageVersion{{Name: "git", Version: "1:2.45.3-1ubuntu2"}}, nil
	}
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.InspectPackages(context.Background(), []string{"git"}); err == nil || called != 0 {
		t.Fatalf("inspection before READY = %v, calls=%d", err, called)
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	packages, err := f.owner.InspectPackages(context.Background(), []string{"git"})
	if err != nil || len(packages) != 1 || packages[0].Name != "git" || called != 1 {
		t.Fatalf("ready inspection = %#v, calls=%d, err=%v", packages, called, err)
	}
	client.probe = func(sshx.Connection) error { return errors.New("probe failed") }
	if _, err := f.owner.InspectPackages(context.Background(), []string{"git"}); err == nil || called != 1 {
		t.Fatalf("inspection after readiness loss = %v, calls=%d", err, called)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestOwnerIdentityInspectionRequiresCurrentReadyGeneration(t *testing.T) {
	f, client := readyFixture(t)
	called := 0
	client.identity = func(connection sshx.Connection) (sshx.GuestIdentity, error) {
		called++
		if connection.Binding.SessionID != f.record.ID {
			t.Fatalf("foreign identity binding: %+v", connection.Binding)
		}
		return sshx.GuestIdentity{MachineID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Hostname: "boxwarden-bbbbbbbbbbbb"}, nil
	}
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.InspectIdentity(context.Background()); err == nil || called != 0 {
		t.Fatalf("identity inspection before READY = %v, calls=%d", err, called)
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	identity, err := f.owner.InspectIdentity(context.Background())
	if err != nil || identity.MachineID != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" || called != 1 {
		t.Fatalf("identity inspection = %+v, calls=%d, err=%v", identity, called, err)
	}
	client.probe = func(sshx.Connection) error { return errors.New("probe failed") }
	if _, err := f.owner.InspectIdentity(context.Background()); err == nil || called != 1 {
		t.Fatalf("identity query after readiness loss = %v, calls=%d", err, called)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestReadyRequiresExactBootstrapAndFailsClosedOnConvergence(t *testing.T) {
	f, client := readyFixture(t)
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err == nil {
		t.Fatal("READY accepted before exact serial pin")
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	client.apply = func(sshx.Connection, string) error { return errors.New("zone apply failed") }
	if err := f.owner.Ready(context.Background()); err == nil {
		t.Fatal("READY accepted failed zone convergence")
	}
	if snapshot := f.owner.Snapshot(context.Background()); snapshot.CertificateCurrent || snapshot.ProbeOK || snapshot.ZoneMatches {
		t.Fatalf("failed convergence published READY facts: %#v", snapshot)
	}
	_ = f.owner.Stop(context.Background())
	_ = f.owner.Wait(context.Background())
}

func TestReadyRenewsGenerationCertificateBeforeItBecomesNoncurrent(t *testing.T) {
	f, _ := readyFixture(t)
	var nowNanos atomic.Int64
	nowNanos.Store(time.Now().UnixNano())
	now := func() time.Time { return time.Unix(0, nowNanos.Load()) }
	f.owner.deps.now = now
	f.owner.deps.renewInterval = 10 * time.Millisecond
	var issues atomic.Int32
	f.owner.deps.issuer = func(sshx.CAIdentity) certificateIssuer {
		return readyIssuer(func(_ context.Context, binding sshx.Binding, _, key string) (sshx.Certificate, error) {
			count := issues.Add(1)
			path := key + "-cert.pub"
			if err := os.WriteFile(path, []byte("cert"), 0o644); err != nil {
				return sshx.Certificate{}, err
			}
			lifetime := 6 * time.Minute
			if count > 1 {
				lifetime = 15 * time.Minute
			}
			return sshx.Certificate{Path: path, Identity: binding.CertificateIdentity(), Principal: binding.Principal(), NotAfter: now().Add(lifetime)}, nil
		})
	}
	if err := f.owner.Start(context.Background(), f.request); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.owner.Ready(context.Background()); err != nil {
		t.Fatal(err)
	}
	nowNanos.Add(int64(2 * time.Minute))
	deadline := time.After(2 * time.Second)
	for {
		snapshot := f.owner.Snapshot(context.Background())
		if issues.Load() >= 2 && snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("certificate was not renewed after entering the renewal window: issues=%d snapshot=%#v", issues.Load(), snapshot)
		case <-time.After(10 * time.Millisecond):
		}
	}
	_ = f.owner.Stop(context.Background())
	if err := f.owner.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
}
