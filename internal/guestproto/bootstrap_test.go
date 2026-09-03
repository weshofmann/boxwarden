package guestproto

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls  [][]string
	output string
	err    error
}

func (r *fakeRunner) Run(_ context.Context, path string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{path}, args...))
	if r.err != nil {
		return nil, r.err
	}
	if len(args) > 0 && args[0] == "-t" {
		return nil, nil
	}
	return []byte(r.output), nil
}

func sshdOutput() string {
	return strings.Join([]string{
		"trustedusercakeys /etc/ssh/boxwarden/active/trusted-user-ca.pub",
		"authorizedprincipalsfile /etc/ssh/boxwarden/active/authorized_principals/%u",
		"authorizedkeysfile none", "permituserenvironment no", "permituserrc no", "passwordauthentication no", "kbdinteractiveauthentication no", "permitrootlogin no", "x11forwarding no", "allowtcpforwarding no", "allowstreamlocalforwarding no", "permittunnel no", ""}, "\n")
}

func TestSerialPublishesExactStateAndLaterGenerationIsIdempotent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "etc/ssh/boxwarden")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub")
	if err := os.WriteFile(host, []byte(testKey+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{output: sshdOutput()}
	bootstrapper := NewBootstrapper(root, runner)
	request := testSerialRequest()
	result, err := bootstrapper.Serial(context.Background(), request)
	if err != nil {
		t.Fatalf("Serial(): %v", err)
	}
	if result.HostPublicKey != testKey || len(result.InstalledSHA256) != 3 {
		t.Fatalf("result = %#v", result)
	}
	manifest, err := os.ReadFile(filepath.Join(parent, "active/management-binding.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "nonce") || strings.Contains(string(manifest), "generation") {
		t.Fatalf("manifest persisted correlation fields: %s", manifest)
	}
	request.StartGeneration = "9a7b5b4b-8746-4430-9a74-04ab1ced127e"
	if _, err := bootstrapper.Serial(context.Background(), request); err != nil {
		t.Fatalf("later generation exact retry: %v", err)
	}
	request.CAPublicKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAB"
	request.CAFingerprint = testFingerprint(request.CAPublicKey)
	if _, err := bootstrapper.Serial(context.Background(), request); err == nil {
		t.Fatal("changed anchor replaced active state")
	}
}

func TestSerialRejectsUnsafeOrStaleTargetBeforeSSHD(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "etc/ssh/boxwarden")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, ".boxwarden-staging-old"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{output: sshdOutput()}
	bootstrapper := NewBootstrapper(root, runner)
	if _, err := bootstrapper.Serial(context.Background(), testSerialRequest()); err == nil {
		t.Fatal("stale staging accepted")
	}
	if len(runner.calls) != 0 {
		t.Fatal("sshd executed after stale-stage rejection")
	}
}

func TestManagementOnlyRunsFixedTimezoneProgram(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "etc/ssh/boxwarden")
	if err := os.MkdirAll(filepath.Join(parent, "active/authorized_principals"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		name, data string
		mode       os.FileMode
	}{{"trusted-user-ca.pub", testKey + "\n", 0o644}, {"authorized_principals/boxwarden", "boxwarden-session-" + testSession + "\n", 0o644}, {"management-binding.json", `{"version":1,"domain":"work","session_id":"` + testSession + `","backend_kind":"tart","backend_object":"workstation","ca_fingerprint":"` + testFingerprint(testKey) + `","principal":"boxwarden-session-` + testSession + `"}` + "\n", 0o600}} {
		if err := os.WriteFile(filepath.Join(parent, "active", entry.name), []byte(entry.data), entry.mode); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(filepath.Join(parent, "active", entry.name), entry.mode)
	}
	runner := &fakeRunner{}
	bootstrapper := NewBootstrapper(root, runner)
	request := ManagementRequest{Version: Version, Kind: "apply_zone", Association: testSerialRequest().Association, Zone: "America/Chihuahua"}
	if _, err := bootstrapper.Management(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 1 || strings.Join(runner.calls[0], " ") != "/usr/bin/timedatectl set-timezone America/Chihuahua" {
		t.Fatalf("runner calls = %#v", runner.calls)
	}
	runner.err = errors.New("unexpected")
	request.Kind = "probe"
	request.Zone = ""
	if _, err := bootstrapper.Management(context.Background(), request); err != nil {
		t.Fatalf("probe uses no program: %v", err)
	}
}

func TestPublicationFailuresLeaveNoPartialActiveAndPermitCleanRetry(t *testing.T) {
	for _, point := range []string{"before-ca", "before-principal", "before-manifest", "before-publish"} {
		t.Run(point, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "etc/ssh/boxwarden")
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			host := filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub")
			if err := os.WriteFile(host, []byte(testKey+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			bootstrapper := NewBootstrapper(root, &fakeRunner{output: sshdOutput()})
			bootstrapper.Failpoint = func(candidate string) error {
				if candidate == point {
					return errors.New("interrupted")
				}
				return nil
			}
			if _, err := bootstrapper.Serial(context.Background(), testSerialRequest()); err == nil {
				t.Fatal("publication interruption accepted")
			}
			if _, err := os.Lstat(filepath.Join(parent, "active")); !os.IsNotExist(err) {
				t.Fatalf("partial active tree remains: %v", err)
			}
			entries, err := os.ReadDir(parent)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("stale stage remains: %#v", entries)
			}
			bootstrapper.Failpoint = nil
			if _, err := bootstrapper.Serial(context.Background(), testSerialRequest()); err != nil {
				t.Fatalf("clean retry: %v", err)
			}
		})
	}
}

func TestSerialRejectsMissingMalformedHostKeyAndEveryEffectiveSSHDMismatch(t *testing.T) {
	for _, host := range []string{"", "not-a-key\n"} {
		t.Run("host="+host, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "etc/ssh/boxwarden")
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(parent, 0o755)
			if host != "" {
				if err := os.WriteFile(filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub"), []byte(host), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewBootstrapper(root, &fakeRunner{output: sshdOutput()}).Serial(context.Background(), testSerialRequest()); err == nil {
				t.Fatal("invalid host key accepted")
			}
		})
	}
	for _, field := range []string{"trustedusercakeys", "authorizedprincipalsfile", "authorizedkeysfile", "permituserenvironment", "permituserrc", "passwordauthentication", "kbdinteractiveauthentication", "permitrootlogin", "x11forwarding", "allowtcpforwarding", "allowstreamlocalforwarding", "permittunnel"} {
		t.Run("sshd="+field, func(t *testing.T) {
			root := t.TempDir()
			parent := filepath.Join(root, "etc/ssh/boxwarden")
			if err := os.MkdirAll(parent, 0o755); err != nil {
				t.Fatal(err)
			}
			_ = os.Chmod(parent, 0o755)
			if err := os.WriteFile(filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub"), []byte(testKey+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			output := strings.Replace(sshdOutput(), field+" ", field+" wrong-", 1)
			if _, err := NewBootstrapper(root, &fakeRunner{output: output}).Serial(context.Background(), testSerialRequest()); err == nil {
				t.Fatal("effective sshd mismatch accepted")
			}
		})
	}
}

func TestManagementRejectsAmbiguousDurableManifest(t *testing.T) {
	for _, replacement := range []string{
		`{"version":1,"version":1}`,
		`{"version":1,"domain":"work","session_id":"123e4567-e89b-42d3-a456-426614174000","backend_kind":"tart","backend_object":"workstation","ca_fingerprint":"SHA256:bad","principal":"boxwarden-session-123e4567-e89b-42d3-a456-426614174000","unknown":true}`,
		`{} {}`,
	} {
		root := t.TempDir()
		parent := filepath.Join(root, "etc/ssh/boxwarden")
		if err := os.MkdirAll(parent, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(parent, 0o755)
		if err := os.WriteFile(filepath.Join(root, "etc/ssh/ssh_host_ed25519_key.pub"), []byte(testKey+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		bootstrapper := NewBootstrapper(root, &fakeRunner{output: sshdOutput()})
		if _, err := bootstrapper.Serial(context.Background(), testSerialRequest()); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(parent, "active/management-binding.json")
		if err := os.WriteFile(path, []byte(replacement), 0o600); err != nil {
			t.Fatal(err)
		}
		_ = os.Chmod(path, 0o600)
		request := ManagementRequest{Version: Version, Kind: "probe", Association: testSerialRequest().Association}
		if _, err := bootstrapper.Management(context.Background(), request); err == nil {
			t.Fatalf("ambiguous manifest accepted: %s", replacement)
		}
	}
}

func TestCanceledContextDoesNotPublishOrRunGuestPrograms(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "etc/ssh/boxwarden")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(parent, 0o755)
	runner := &fakeRunner{output: sshdOutput()}
	bootstrapper := NewBootstrapper(root, runner)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := bootstrapper.Serial(ctx, testSerialRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Serial cancellation = %v", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("canceled serial invoked runner: %#v", runner.calls)
	}
	if _, err := os.Lstat(filepath.Join(parent, "active")); !os.IsNotExist(err) {
		t.Fatalf("canceled serial published active: %v", err)
	}
}
