package guestproto

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The image starts with conservative guest defaults while the live bootstrap
// only requires the CA/principal settings needed by Boxwarden management.
var goldenSSHD = map[string]string{
	"pubkeyauthentication": "yes",
	"trustedusercakeys": "/etc/ssh/boxwarden/active/trusted-user-ca.pub",
	"authorizedprincipalsfile": "/etc/ssh/boxwarden/active/authorized_principals/%u",
	"authorizedkeysfile": ".ssh/authorized_keys",
	"permituserenvironment": "no",
	"permituserrc": "no",
	"passwordauthentication": "no",
	"kbdinteractiveauthentication": "no",
	"permitrootlogin": "no",
	"x11forwarding": "no",
	"allowagentforwarding": "no",
	"allowtcpforwarding": "no",
	"allowstreamlocalforwarding": "no",
	"gatewayports": "no",
	"permittunnel": "no",
}

// A golden built from autoinstall must satisfy the same effective sshd contract
// that serial bootstrap checks before publishing domain trust. This compares
// the tracked generated drop-in with the production guard, not a second copy of
// expected policy in a fixture.
func TestGoldenSSHDDefinitionMatchesBootstrapContract(t *testing.T) {
	path := filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64", "autoinstall", "user-data")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(contents), "\n")
	inside := false
	settings := map[string]string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "cat > /target/etc/ssh/sshd_config.d/") && strings.HasSuffix(trimmed, "<<'EOF'") {
			if inside || len(settings) != 0 {
				t.Fatal("multiple generated sshd drop-ins")
			}
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if trimmed == "EOF" {
			inside = false
			break
		}
		parts := strings.Fields(trimmed)
		if len(parts) != 2 {
			t.Fatalf("invalid generated sshd directive %q", trimmed)
		}
		key := strings.ToLower(parts[0])
		if _, exists := settings[key]; exists {
			t.Fatalf("duplicate generated sshd directive %s", key)
		}
		settings[key] = parts[1]
	}
	if inside || len(settings) == 0 {
		t.Fatal("generated sshd drop-in missing or unterminated")
	}
	for key, want := range goldenSSHD {
		if got := settings[key]; got != want {
			t.Errorf("generated sshd %s = %q, bootstrap requires %q", key, got, want)
		}
	}
	if len(settings) != len(goldenSSHD) {
		t.Errorf("generated sshd has %d directives, want %d", len(settings), len(goldenSSHD))
	}
}

func TestGoldenFinalizerSSHDGuardMatchesBootstrapContract(t *testing.T) {
	path := filepath.Join("..", "..", "guest", "ubuntu-24.04-arm64", "finalize-golden.sh")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inside := false
	settings := map[string]string{}
	for _, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "done <<'SSHD_EXPECTED'" {
			if inside || len(settings) != 0 {
				t.Fatal("multiple finalizer sshd guards")
			}
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if trimmed == "SSHD_EXPECTED" {
			inside = false
			break
		}
		parts := strings.Fields(trimmed)
		if len(parts) != 2 {
			t.Fatalf("invalid finalizer sshd guard %q", trimmed)
		}
		if _, exists := settings[parts[0]]; exists {
			t.Fatalf("duplicate finalizer sshd guard %s", parts[0])
		}
		settings[parts[0]] = parts[1]
	}
	if inside || len(settings) == 0 {
		t.Fatal("finalizer sshd guard missing or unterminated")
	}
	for key, want := range goldenSSHD {
		if got := settings[key]; got != want {
			t.Errorf("finalizer sshd %s = %q, bootstrap requires %q", key, got, want)
		}
	}
	if len(settings) != len(goldenSSHD) {
		t.Errorf("finalizer sshd guard has %d directives, want %d", len(settings), len(goldenSSHD))
	}
}
