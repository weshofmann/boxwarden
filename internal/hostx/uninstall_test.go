package hostx

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstallRefusesActiveOrUnverifiableConsumers(t *testing.T) {
	for name, checker := range map[string]ConsumerChecker{
		"active":  consumerFake{active: true},
		"unknown": consumerFake{err: errors.New("inventory unavailable")},
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckUninstallable(context.Background(), SoftnetExecutableSHA256, checker); err == nil {
				t.Fatal("CheckUninstallable() error = nil, want refusal")
			}
		})
	}
	if err := CheckUninstallable(context.Background(), SoftnetExecutableSHA256, consumerFake{}); err != nil {
		t.Fatalf("CheckUninstallable(inactive) error = %v", err)
	}
	if err := CheckUninstallable(context.Background(), "0.19.0", consumerFake{}); err == nil {
		t.Fatal("CheckUninstallable(version selector) error = nil, want exact digest refusal")
	}
}

func TestRootedUninstallerRemovesOnlyExactInactiveTreeAndFsyncsParent(t *testing.T) {
	publisher, caller, group := installedUninstallFixture(t)
	parent := filepath.Dir(publisher.finalDir())
	adjacent := filepath.Join(parent, strings.Repeat("f", 64))
	if err := os.Mkdir(adjacent, 0o755); err != nil {
		t.Fatal(err)
	}
	checker := &recordingConsumer{}
	var synced string
	uninstaller := RootedUninstaller{
		Publisher: publisher,
		Consumers: checker,
		syncParent: func(path string) error {
			synced = path
			return syncDirectory(path)
		},
	}

	if err := uninstaller.Uninstall(t.Context(), SoftnetExecutableSHA256, caller, group); err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if _, err := os.Lstat(publisher.finalDir()); !os.IsNotExist(err) {
		t.Fatalf("exact digest tree still exists after uninstall: %v", err)
	}
	if info, err := os.Lstat(adjacent); err != nil || !info.IsDir() {
		t.Fatalf("adjacent digest tree was broadened into removal: %v, %v", info, err)
	}
	if checker.calls != 1 || checker.digest != SoftnetExecutableSHA256 {
		t.Fatalf("consumer query = calls %d digest %q, want one exact query", checker.calls, checker.digest)
	}
	if synced != parent {
		t.Fatalf("synced parent = %q, want %q", synced, parent)
	}
}

func TestRootedUninstallerRefusesEveryNonExactSelectorWithoutMutation(t *testing.T) {
	publisher, caller, group := installedUninstallFixture(t)
	checker := &recordingConsumer{}
	for _, selector := range []string{"", SoftnetVersion, SoftnetExecutableSHA256[:32], strings.Repeat("0", 64)} {
		if err := (RootedUninstaller{Publisher: publisher, Consumers: checker}).Uninstall(t.Context(), selector, caller, group); err == nil {
			t.Fatalf("Uninstall(%q) error = nil, want exact full digest refusal", selector)
		}
		if _, err := os.Lstat(publisher.finalDir()); err != nil {
			t.Fatalf("exact tree changed after selector %q refusal: %v", selector, err)
		}
	}
	if checker.calls != 0 {
		t.Fatalf("consumer checks = %d, want zero for invalid selectors", checker.calls)
	}
}

func TestRootedUninstallerRefusesActiveOrUnverifiableConsumerWithoutRemoval(t *testing.T) {
	for name, checker := range map[string]*recordingConsumer{
		"active":       {active: true},
		"unverifiable": {err: errors.New("private inventory detail")},
	} {
		t.Run(name, func(t *testing.T) {
			publisher, caller, group := installedUninstallFixture(t)
			err := (RootedUninstaller{Publisher: publisher, Consumers: checker}).Uninstall(t.Context(), SoftnetExecutableSHA256, caller, group)
			if err == nil {
				t.Fatal("Uninstall() error = nil, want consumer refusal")
			}
			if _, statErr := os.Lstat(publisher.finalDir()); statErr != nil {
				t.Fatalf("validated tree changed on consumer refusal: %v", statErr)
			}
		})
	}
}

func TestRootedUninstallerStrictlyValidatesTreeBeforeCheckingConsumers(t *testing.T) {
	for name, makeUnsafe := range map[string]func(*testing.T, *RootedPublisher){
		"ancestor mode": func(t *testing.T, publisher *RootedPublisher) {
			t.Helper()
			if err := os.Chmod(filepath.Join(publisher.Root, "toolchains"), 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"ancestor ownership": func(_ *testing.T, publisher *RootedPublisher) {
			publisher.rootGID++
		},
		"extended ACL": func(_ *testing.T, publisher *RootedPublisher) {
			publisher.acl = pathACLInspector{publisher.finalDir(): true}
		},
		"unexpected entry": func(t *testing.T, publisher *RootedPublisher) {
			t.Helper()
			if err := os.WriteFile(filepath.Join(publisher.finalDir(), "unexpected"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"softnet digest": func(t *testing.T, publisher *RootedPublisher) {
			t.Helper()
			path := filepath.Join(publisher.finalDir(), "softnet")
			if err := os.Chmod(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("drifted bytes"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o550); err != nil {
				t.Fatal(err)
			}
		},
		"manifest bytes": func(t *testing.T, publisher *RootedPublisher) {
			t.Helper()
			path := filepath.Join(publisher.finalDir(), "manifest.json")
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o400); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			publisher, caller, group := installedUninstallFixture(t)
			makeUnsafe(t, &publisher)
			checker := &recordingConsumer{}
			err := (RootedUninstaller{Publisher: publisher, Consumers: checker}).Uninstall(t.Context(), SoftnetExecutableSHA256, caller, group)
			if err == nil {
				t.Fatal("Uninstall() error = nil, want unsafe-tree refusal")
			}
			if checker.calls != 0 {
				t.Fatalf("consumer checks = %d, want zero before unsafe-tree refusal", checker.calls)
			}
			if _, statErr := os.Lstat(publisher.finalDir()); statErr != nil {
				t.Fatalf("unsafe tree was removed: %v", statErr)
			}
		})
	}
}

func TestRootedUninstallerRefusesCallerOrGroupBindingMismatch(t *testing.T) {
	for name, alter := range map[string]func(*Caller, *Group){
		"caller": func(caller *Caller, _ *Group) { caller.Name = "other" },
		"group":  func(_ *Caller, group *Group) { group.ID++ },
	} {
		t.Run(name, func(t *testing.T) {
			publisher, caller, group := installedUninstallFixture(t)
			alter(&caller, &group)
			checker := &recordingConsumer{}
			if err := (RootedUninstaller{Publisher: publisher, Consumers: checker}).Uninstall(t.Context(), SoftnetExecutableSHA256, caller, group); err == nil {
				t.Fatal("Uninstall() error = nil, want manifested binding refusal")
			}
			if checker.calls != 0 {
				t.Fatalf("consumer checks = %d, want zero before binding refusal", checker.calls)
			}
			if _, err := os.Lstat(publisher.finalDir()); err != nil {
				t.Fatalf("tree changed after binding refusal: %v", err)
			}
		})
	}
}

func TestRootedUninstallerRevalidatesAfterConsumerObservation(t *testing.T) {
	publisher, caller, group := installedUninstallFixture(t)
	checker := &recordingConsumer{hook: func() error {
		path := filepath.Join(publisher.finalDir(), "softnet")
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("racing replacement"), 0o700)
	}}

	if err := (RootedUninstaller{Publisher: publisher, Consumers: checker}).Uninstall(t.Context(), SoftnetExecutableSHA256, caller, group); err == nil {
		t.Fatal("Uninstall() error = nil, want post-consumer race refusal")
	}
	if _, err := os.Lstat(publisher.finalDir()); err != nil {
		t.Fatalf("racing tree was removed: %v", err)
	}
}

func installedUninstallFixture(t *testing.T) (RootedPublisher, Caller, Group) {
	t.Helper()
	root, source, digest := publisherFixture(t)
	publisher := testPublisher(root, digest)
	caller := Caller{UID: os.Getuid(), Name: "operator", Home: filepath.Join(filepath.Dir(source), "home")}
	group := Group{ID: os.Getgid(), Name: OperatorGroupName, Members: []int{caller.UID}}
	if err := publisher.Publish(t.Context(), publisherRequest(source), caller, group); err != nil {
		t.Fatalf("install uninstall fixture: %v", err)
	}
	return publisher, caller, group
}

type consumerFake struct {
	active bool
	err    error
}

func (f consumerFake) HasConsumer(_ context.Context, _ string) (bool, error) { return f.active, f.err }

type recordingConsumer struct {
	active bool
	err    error
	hook   func() error
	calls  int
	digest string
}

func (f *recordingConsumer) HasConsumer(_ context.Context, digest string) (bool, error) {
	f.calls++
	f.digest = digest
	if f.hook != nil {
		if err := f.hook(); err != nil {
			return false, err
		}
	}
	return f.active, f.err
}
