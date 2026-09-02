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
	certificateLifetime            = 15 * time.Minute
	renewalWindow                  = 5 * time.Minute
	certificateSchedulingTolerance = 2 * time.Second
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
	ctx, cancel := boundedCAContext(ctx)
	defer cancel()
	if i == nil || i.runner == nil || i.identity == nil || i.now == nil {
		return Certificate{}, fmt.Errorf("certificate issuer dependencies are required")
	}
	if err := binding.Validate(); err != nil {
		return Certificate{}, err
	}
	if !filepath.IsAbs(clientKeyPath) || filepath.Clean(clientKeyPath) != clientKeyPath {
		return Certificate{}, fmt.Errorf("client key path must be canonical and absolute")
	}
	if err := requirePrivateDirectory(filepath.Dir(clientKeyPath)); err != nil {
		return Certificate{}, fmt.Errorf("validate client-key directory: %w", err)
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
	actual, valid := parseCertificateInspection(inspection.Stdout)
	if err != nil || inspection.Truncated || !valid || !certificateInspectionMatches(actual, certificate) {
		return Certificate{}, fmt.Errorf("issued management certificate inspection failed")
	}
	certificate.NotBefore, certificate.NotAfter = actual.NotBefore, actual.NotAfter
	return certificate, nil
}

func validCertificateInspection(output string, certificate Certificate) bool {
	actual, valid := parseCertificateInspection(output)
	return valid && certificateInspectionMatches(actual, certificate)
}

func parseCertificateInspection(output string) (Certificate, bool) {
	if len(output) == 0 || len(output) > maxStateFileBytes {
		return Certificate{}, false
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	seen := map[string]bool{}
	principalIndex := -1
	var certificate Certificate
	for index, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Type:"):
			if seen["type"] || line != "Type: ssh-ed25519-cert-v01@openssh.com user certificate" {
				return Certificate{}, false
			}
			seen["type"] = true
		case strings.HasPrefix(line, "Key ID:"):
			if seen["id"] || !strings.HasPrefix(line, "Key ID: \"") || !strings.HasSuffix(line, "\"") {
				return Certificate{}, false
			}
			seen["id"] = true
			certificate.Identity = strings.TrimSuffix(strings.TrimPrefix(line, "Key ID: \""), "\"")
		case strings.HasPrefix(line, "Valid:"):
			if seen["valid"] || !strings.HasPrefix(line, "Valid: from ") {
				return Certificate{}, false
			}
			validity := strings.TrimPrefix(line, "Valid: from ")
			parts := strings.Split(validity, " to ")
			if len(parts) != 2 {
				return Certificate{}, false
			}
			var err error
			certificate.NotBefore, err = time.Parse("2006-01-02T15:04:05", parts[0])
			if err != nil {
				return Certificate{}, false
			}
			certificate.NotAfter, err = time.Parse("2006-01-02T15:04:05", parts[1])
			if err != nil {
				return Certificate{}, false
			}
			seen["valid"] = true
		case line == "Principals:":
			if seen["principal"] || index+1 >= len(lines) || strings.TrimSpace(lines[index+1]) == "" {
				return Certificate{}, false
			}
			seen["principal"] = true
			principalIndex = index
			certificate.Principal = strings.TrimSpace(lines[index+1])
		case strings.HasPrefix(line, "Critical Options:"):
			if seen["critical"] || line != "Critical Options: (none)" {
				return Certificate{}, false
			}
			seen["critical"] = true
		case strings.HasPrefix(line, "Extensions:"):
			if seen["extensions"] || line != "Extensions: (none)" {
				return Certificate{}, false
			}
			seen["extensions"] = true
		}
	}
	if !(seen["type"] && seen["id"] && seen["valid"] && seen["principal"] && seen["critical"] && seen["extensions"] && principalIndex+2 < len(lines) && strings.TrimSpace(lines[principalIndex+2]) == "Critical Options: (none)") {
		return Certificate{}, false
	}
	return certificate, true
}

func certificateInspectionMatches(actual, requested Certificate) bool {
	if actual.Identity != requested.Identity || actual.Principal != requested.Principal || actual.NotAfter.Sub(actual.NotBefore) != certificateLifetime+renewalWindow {
		return false
	}
	return durationWithin(actual.NotBefore.Sub(requested.NotBefore.UTC()), certificateSchedulingTolerance) && durationWithin(actual.NotAfter.Sub(requested.NotAfter.UTC()), certificateSchedulingTolerance)
}

func durationWithin(value, limit time.Duration) bool {
	if value < 0 {
		value = -value
	}
	return value <= limit
}

// RenewalRequired is pure policy: certificates are refreshed at or inside the fixed five-minute window.
func RenewalRequired(certificate Certificate, now time.Time) bool {
	return !certificate.NotAfter.After(now.Add(renewalWindow))
}
