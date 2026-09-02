package sshx

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	certificateLifetime = 15 * time.Minute
	renewalWindow       = 5 * time.Minute
)

// Binding is the durable association that a certificate and host-key pin must match.
type Binding struct {
	Domain        domain.ID
	SessionID     string
	BackendKind   string
	BackendObject string
}

func (b Binding) Validate() error {
	if _, err := domain.Parse(string(b.Domain)); err != nil {
		return err
	}
	if !validUUID(b.SessionID) || b.BackendKind == "" || b.BackendObject == "" {
		return fmt.Errorf("invalid SSH management binding")
	}
	return nil
}

func (b Binding) Principal() string { return "boxwarden-session-" + b.SessionID }
func (b Binding) CertificateIdentity() string {
	return "boxwarden:" + string(b.Domain) + ":" + b.SessionID
}

type Certificate struct {
	Path      string
	Identity  string
	Principal string
	NotBefore time.Time
	NotAfter  time.Time
}

type CertificateIssuer struct {
	ca       CAIdentity
	runner   Runner
	identity Identity
	now      func() time.Time
}

// NewCertificateIssuer receives an explicit, domain-bound CA handle loaded by
// trusted-host code. It deliberately has no configured-domain lookup or fallback.
func NewCertificateIssuer(ca CAIdentity, runner Runner, identity Identity, now func() time.Time) *CertificateIssuer {
	return &CertificateIssuer{ca: ca, runner: runner, identity: identity, now: now}
}

// Issue signs only the derived per-session principal with a fixed validity and no extensions.
func (i *CertificateIssuer) Issue(ctx context.Context, binding Binding, clientKeyPath string) (Certificate, error) {
	if i == nil || i.runner == nil || i.identity == nil || i.now == nil {
		return Certificate{}, fmt.Errorf("certificate issuer dependencies are required")
	}
	if err := binding.Validate(); err != nil {
		return Certificate{}, err
	}
	if !filepath.IsAbs(clientKeyPath) || filepath.Clean(clientKeyPath) != clientKeyPath {
		return Certificate{}, fmt.Errorf("client key path must be canonical and absolute")
	}
	if _, err := requirePrivateFile(clientKeyPath); err != nil {
		return Certificate{}, fmt.Errorf("validate client key: %w", err)
	}
	if i.ca.Domain != binding.Domain || i.ca.StateRoot == "" {
		return Certificate{}, fmt.Errorf("loaded CA domain does not match certificate binding")
	}
	store := NewCAStore(CAStoreOptions{Runner: i.runner, Identity: i.identity, SSHKeygenPath: i.ca.SSHKeygenPath})
	ca, err := store.Load(ctx, Domain{ID: i.ca.Domain, StateRoot: i.ca.StateRoot})
	if err != nil {
		return Certificate{}, err
	}
	now := i.now().UTC()
	certificate := Certificate{Path: clientKeyPath + "-cert.pub", Identity: binding.CertificateIdentity(), Principal: binding.Principal(), NotBefore: now.Add(-renewalWindow), NotAfter: now.Add(certificateLifetime)}
	if ca.CreationUUID != i.ca.CreationUUID || ca.CreatorUID != i.ca.CreatorUID || ca.CreatorName != i.ca.CreatorName || ca.Fingerprint != i.ca.Fingerprint {
		return Certificate{}, fmt.Errorf("loaded CA changed before issuance")
	}
	result, err := i.runner.Run(ctx, Command{Path: store.sshKeygenPath, Args: []string{"-s", ca.PrivateKeyPath, "-I", certificate.Identity, "-n", certificate.Principal, "-V", "-5m:+15m", "-O", "clear", clientKeyPath}})
	if err != nil {
		return Certificate{}, fmt.Errorf("issue management certificate: %w", err)
	}
	if result.Truncated {
		return Certificate{}, fmt.Errorf("issue management certificate produced oversized output")
	}
	if err := normalizePublicFile(certificate.Path); err != nil {
		return Certificate{}, fmt.Errorf("validate issued management certificate: %w", err)
	}
	inspection, err := i.runner.Run(ctx, Command{Path: store.sshKeygenPath, Args: []string{"-L", "-f", certificate.Path}})
	if err != nil || inspection.Truncated || !validCertificateInspection(inspection.Stdout, certificate) {
		return Certificate{}, fmt.Errorf("issued management certificate inspection failed")
	}
	return certificate, nil
}

func validCertificateInspection(output string, certificate Certificate) bool {
	if len(output) == 0 || len(output) > maxStateFileBytes {
		return false
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	seen := map[string]bool{}
	principalIndex := -1
	for index, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Type:"):
			if seen["type"] || line != "Type: ssh-ed25519-cert-v01@openssh.com user certificate" {
				return false
			}
			seen["type"] = true
		case strings.HasPrefix(line, "Key ID:"):
			if seen["id"] || line != "Key ID: \""+certificate.Identity+"\"" {
				return false
			}
			seen["id"] = true
		case strings.HasPrefix(line, "Valid:"):
			want := "Valid: from " + certificate.NotBefore.UTC().Format("2006-01-02T15:04:05") + " to " + certificate.NotAfter.UTC().Format("2006-01-02T15:04:05")
			if seen["valid"] || line != want {
				return false
			}
			seen["valid"] = true
		case line == "Principals:":
			if seen["principal"] || index+1 >= len(lines) || strings.TrimSpace(lines[index+1]) != certificate.Principal {
				return false
			}
			seen["principal"] = true
			principalIndex = index
		case strings.HasPrefix(line, "Critical Options:"):
			if seen["critical"] || line != "Critical Options: (none)" {
				return false
			}
			seen["critical"] = true
		case strings.HasPrefix(line, "Extensions:"):
			if seen["extensions"] || line != "Extensions: (none)" {
				return false
			}
			seen["extensions"] = true
		}
	}
	return seen["type"] && seen["id"] && seen["valid"] && seen["principal"] && seen["critical"] && seen["extensions"] && principalIndex+2 < len(lines) && strings.TrimSpace(lines[principalIndex+2]) == "Critical Options: (none)"
}

// RenewalRequired is pure policy: certificates are refreshed at or inside the fixed five-minute window.
func RenewalRequired(certificate Certificate, now time.Time) bool {
	return !certificate.NotAfter.After(now.Add(renewalWindow))
}
