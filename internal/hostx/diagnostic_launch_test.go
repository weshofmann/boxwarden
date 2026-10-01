//go:build n1diagnostic && !n1candidate

package hostx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Removing mandatory SH or closing the descriptor before Release permits EX
// and breaks this control. No privileged metadata is created in these fixtures.
func TestDiagnosticGuardSharedLifetimeAndExclusiveRefusal(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	g, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Release()
	ex, err := openNoFollow(filepath.Join(p.finalDir(), "launch.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()
	if err = syscall.Flock(int(ex.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("EX passed active SH")
	}
	if err = g.Revalidate(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = g.Release(); err != nil {
		t.Fatal(err)
	}
	if err = g.Revalidate(t.Context()); !errors.Is(err, ErrDiagnosticGuardClosed) {
		t.Fatalf("closed guard = %v", err)
	}
	if err = syscall.Flock(int(ex.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		t.Fatal("EX passed independent SH")
	}
	if err = second.Release(); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Flock(int(ex.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err = acquireDiagnosticTree(t.Context(), p, m, nil); !errors.Is(err, ErrDiagnosticLaunchBusy) {
		t.Fatalf("exclusive refusal = %v", err)
	}
}

func TestDiagnosticGuardRejectsSwappedOrUnlinkedLockAfterAcquire(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "unlink", true: "replace"}[replacement], func(t *testing.T) {
			p, m := diagnosticGuardFixture(t)
			lock := filepath.Join(p.finalDir(), "launch.lock")
			hook := func() {
				if err := os.Remove(lock); err != nil {
					t.Fatal(err)
				}
				if replacement {
					if err := os.WriteFile(lock, nil, 0o440); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, err := acquireDiagnosticTree(t.Context(), p, m, hook); !errors.Is(err, ErrDiagnosticLaunchDrift) {
				t.Fatalf("postacquire change admitted: %v", err)
			}
		})
	}
}

func TestDiagnosticGuardRefusesTreeMetadataAndContentDrift(t *testing.T) {
	cases := map[string]func(*testing.T, *RootedPublisher){
		"extra": func(t *testing.T, p *RootedPublisher) {
			mustWrite(t, filepath.Join(p.finalDir(), "unexpected"), []byte("x"), 0o600)
		},
		"lock bytes": func(t *testing.T, p *RootedPublisher) {
			lock := filepath.Join(p.finalDir(), "launch.lock")
			os.Chmod(lock, 0o600)
			mustWrite(t, lock, []byte("x"), 0o440)
			os.Chmod(lock, 0o440)
		},
		"lock mode": func(t *testing.T, p *RootedPublisher) { os.Chmod(filepath.Join(p.finalDir(), "launch.lock"), 0o640) },
		"lock link": func(t *testing.T, p *RootedPublisher) {
			if err := os.Link(filepath.Join(p.finalDir(), "launch.lock"), filepath.Join(filepath.Dir(p.Root), "other-lock")); err != nil {
				t.Fatal(err)
			}
		},
		"lock symlink": func(t *testing.T, p *RootedPublisher) {
			lock := filepath.Join(p.finalDir(), "launch.lock")
			os.Rename(lock, lock+".old")
			if err := os.Symlink(lock+".old", lock); err != nil {
				t.Fatal(err)
			}
		},
		"lock ACL": func(_ *testing.T, p *RootedPublisher) {
			p.acl = pathACLInspector{filepath.Join(p.finalDir(), "launch.lock"): true}
		},
		"root owner": func(_ *testing.T, p *RootedPublisher) { p.rootUID++ },
		"root group": func(_ *testing.T, p *RootedPublisher) { p.rootGID++ },
		"ancestor mode": func(t *testing.T, p *RootedPublisher) {
			if err := os.Chmod(filepath.Dir(p.finalDir()), 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"executable digest": func(t *testing.T, p *RootedPublisher) {
			path := filepath.Join(p.finalDir(), "softnet")
			os.Chmod(path, 0o600)
			mustWrite(t, path, []byte("changed"), 0o550)
			os.Chmod(path, 0o550)
		},
		"legacy manifest": func(t *testing.T, p *RootedPublisher) { os.Chmod(filepath.Join(p.finalDir(), "manifest.json"), 0o400) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p, m := diagnosticGuardFixture(t)
			mutate(t, &p)
			if _, err := acquireDiagnosticTree(t.Context(), p, m, nil); !errors.Is(err, ErrDiagnosticLaunchDrift) {
				t.Fatalf("drift admitted: %v", err)
			}
		})
	}
}

func TestDiagnosticGuardRevalidationAndCancellation(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := acquireDiagnosticTree(ctx, p, m, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquire: %v", err)
	}
	g, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	if err = g.Revalidate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled revalidation: %v", err)
	}
	lock := filepath.Join(p.finalDir(), "launch.lock")
	if err = os.Rename(lock, lock+".old"); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, lock, nil, 0o440)
	if err = g.Revalidate(t.Context()); !errors.Is(err, ErrDiagnosticLaunchDrift) {
		t.Fatalf("replaced held lock: %v", err)
	}
}

func diagnosticGuardFixture(t *testing.T) (RootedPublisher, Manifest) {
	t.Helper()
	root, source, digest := publisherFixture(t)
	p := testPublisher(root, digest)
	r := publisherRequest(source)
	c := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
	g := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{c.UID}}
	if err := p.Publish(t.Context(), r, c, g); err != nil {
		t.Fatal(err)
	}
	m, err := p.readInstalledManifest()
	if err != nil {
		t.Fatal(err)
	}
	return p, m
}

func mustWrite(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticPublisherLockFailurePrecedesTreeVisibility(t *testing.T) {
	for _, step := range []string{"before-launch-lock", "after-launch-lock"} {
		t.Run(step, func(t *testing.T) {
			root, source, digest := publisherFixture(t)
			p := testPublisher(root, digest)
			p.fail = func(got string) error {
				if got == step {
					return errors.New("injected lock staging failure")
				}
				return nil
			}
			r := publisherRequest(source)
			c := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(root, "home")}
			group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{c.UID}}
			if err := p.Publish(t.Context(), r, c, group); err == nil {
				t.Fatal("publication ignored lock staging failure")
			}
			if _, err := os.Lstat(p.finalDir()); !os.IsNotExist(err) {
				t.Fatalf("digest visible before lock staging completed: %v", err)
			}
		})
	}
}

func TestDiagnosticDoctorRequiresExactlyThreeEntriesAndZeroByteLock(t *testing.T) {
	for _, kind := range []string{"missing", "extra", "nonzero", "mode", "owner", "ACL", "link"} {
		t.Run(kind, func(t *testing.T) {
			i, r := healthyDoctorFixture(t)
			path := filepath.Join(filepath.Dir(QualifiedSoftnetPath), "launch.lock")
			lock := i.paths[path]
			switch kind {
			case "missing":
				delete(i.paths, path)
			case "extra":
				i.directoryNames = []string{"launch.lock", "manifest.json", "softnet", "extra"}
			case "nonzero":
				lock.SHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
				i.paths[path] = lock
			case "mode":
				lock.Mode = 0o640
				i.paths[path] = lock
			case "owner":
				lock.UID++
				i.paths[path] = lock
			case "ACL":
				lock.ExtendedACL = true
				i.paths[path] = lock
			case "link":
				lock.Links = 2
				i.paths[path] = lock
			}
			if report := (SystemDoctor{inspector: i}).Doctor(t.Context(), r); report.Status != Drifted {
				t.Fatalf("diagnostic doctor admitted %s: %#v", kind, report)
			}
		})
	}
}

func TestDiagnosticGuardDescriptorsAreReadOnlyCloseOnExecAndReleasedOnce(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	g, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range g.files {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFL, 0)
		if errno != 0 || flags&syscall.O_ACCMODE != syscall.O_RDONLY {
			t.Fatalf("descriptor not read-only: flags=%x errno=%v", flags, errno)
		}
		flags, _, errno = syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETFD, 0)
		if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
			t.Fatalf("descriptor can leak across exec: flags=%x errno=%v", flags, errno)
		}
	}
	if err = g.Release(); err != nil {
		t.Fatal(err)
	}
	if err = g.Release(); err != nil {
		t.Fatal(err)
	}
	for _, f := range g.files {
		if _, err = f.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatalf("descriptor retained after Release: %v", err)
		}
	}
}

func TestDiagnosticGuardRejectsChangedValidManifestBinding(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	g, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	path := filepath.Join(p.finalDir(), "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := bytes.Replace(data, []byte("2026-09-01T00:00:00Z"), []byte("2026-09-01T00:00:01Z"), 1)
	if _, err := ParseManifest(replacement); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, path, replacement, 0o444)
	if err = os.Chmod(path, 0o444); err != nil {
		t.Fatal(err)
	}
	if err = g.Revalidate(t.Context()); !errors.Is(err, ErrDiagnosticLaunchDrift) {
		t.Fatalf("changed valid manifest admitted: %v", err)
	}
}

// The ordinary recursive remover must never consume a diagnostic tree, even
// when an inventory reports no consumers. Task4b has a separate exact cleanup.
func TestDiagnosticLegacyUninstallerRefusesBeforeInventoryOrMutation(t *testing.T) {
	p, c, g := installedUninstallFixture(t)
	before, err := os.ReadFile(filepath.Join(p.finalDir(), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	checker := &recordingConsumer{}
	synced := false
	err = (RootedUninstaller{Publisher: p, Consumers: checker, syncParent: func(string) error { synced = true; return nil }}).Uninstall(t.Context(), SoftnetExecutableSHA256, c, g)
	if !errors.Is(err, ErrDiagnosticLegacyUninstall) {
		t.Fatalf("legacy recursive uninstaller refusal = %v", err)
	}
	if checker.calls != 0 || synced {
		t.Fatal("refusal happened after inventory or mutation")
	}
	after, err := os.ReadFile(filepath.Join(p.finalDir(), "manifest.json"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("tree changed before refusal: %v", err)
	}
	names, err := os.ReadDir(p.finalDir())
	if err != nil || len(names) != 3 {
		t.Fatalf("tree entries changed: %v", err)
	}
}

func TestDiagnosticGuardReleaseRetainsActualCloseFailure(t *testing.T) {
	p, m := diagnosticGuardFixture(t)
	g, err := acquireDiagnosticTree(t.Context(), p, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A synthetic early close demonstrates that Release preserves failure and
	// still closes the lock. Production never exports these descriptors.
	if err = g.files[1].Close(); err != nil {
		t.Fatal(err)
	}
	first := g.Release()
	if !errors.Is(first, os.ErrClosed) {
		t.Fatalf("close failure hidden: %v", first)
	}
	if second := g.Release(); second != first {
		t.Fatalf("idempotent release changed prior error: %v", second)
	}
	ex, err := openNoFollow(filepath.Join(p.finalDir(), "launch.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()
	if err = syscall.Flock(int(ex.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("lock leaked after other close failure: %v", err)
	}
}
