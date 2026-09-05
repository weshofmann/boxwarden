package sshx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
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
func (i *CertificateIssuer) Issue(ctx context.Context, binding Binding, runtimeDirectory, clientKeyPath string) (Certificate, error) {
	ctx, cancel := boundedCAContext(ctx)
	defer cancel()
	if i == nil || i.runner == nil || i.identity == nil || i.now == nil {
		return Certificate{}, fmt.Errorf("certificate issuer dependencies are required")
	}
	if err := binding.Validate(); err != nil {
		return Certificate{}, err
	}
	if _, err := requireRuntimeFile(runtimeDirectory, clientKeyPath, privateFileMode); err != nil {
		return Certificate{}, fmt.Errorf("validate client key: %w", err)
	}
	certificate := Certificate{Path: clientKeyPath + "-cert.pub", Identity: binding.CertificateIdentity(), Principal: binding.Principal()}
	output, exists, err := admitCertificateOutput(runtimeDirectory, certificate.Path)
	if err != nil {
		return Certificate{}, fmt.Errorf("validate certificate output: %w", err)
	}
	if i.ca.Domain != binding.Domain || i.ca.StateRoot == "" {
		return Certificate{}, fmt.Errorf("loaded CA domain does not match certificate binding")
	}
	store := NewCAStore(CAStoreOptions{Runner: i.runner, Identity: i.identity, SSHKeygenPath: i.ca.SSHKeygenPath})
	ca, err := store.Load(ctx, Domain{ID: i.ca.Domain, StateRoot: i.ca.StateRoot})
	if err != nil {
		return Certificate{}, err
	}
	if ca.CreationUUID != i.ca.CreationUUID || ca.CreatorUID != i.ca.CreatorUID || ca.CreatorName != i.ca.CreatorName || ca.Fingerprint != i.ca.Fingerprint {
		return Certificate{}, fmt.Errorf("loaded CA changed before issuance")
	}
	temporaryKey, temporaryKeyInfo, err := makeTemporaryClientKey(runtimeDirectory, clientKeyPath)
	if err != nil {
		return Certificate{}, fmt.Errorf("prepare temporary certificate input: %w", err)
	}
	defer removeExactFile(temporaryKey, temporaryKeyInfo, privateFileMode)
	temporaryCertificate := temporaryKey + "-cert.pub"
	defer cleanupTemporaryCertificate(runtimeDirectory, temporaryCertificate)
	signingStarted := i.now().UTC()
	certificate.NotBefore, certificate.NotAfter = signingStarted.Add(-renewalWindow), signingStarted.Add(certificateLifetime)
	result, err := i.runner.Run(ctx, Command{Path: store.sshKeygenPath, Args: []string{"-s", ca.PrivateKeyPath, "-I", certificate.Identity, "-n", certificate.Principal, "-V", "-5m:+15m", "-O", "clear", temporaryKey}})
	signingFinished := i.now().UTC()
	if signingFinished.Before(signingStarted) {
		return Certificate{}, fmt.Errorf("trusted clock moved backwards while signing management certificate")
	}
	if err != nil {
		return Certificate{}, fmt.Errorf("issue management certificate: %w", err)
	}
	if result.Truncated {
		return Certificate{}, fmt.Errorf("issue management certificate produced oversized output")
	}
	if err := normalizePublicFile(temporaryCertificate); err != nil {
		return Certificate{}, fmt.Errorf("validate issued management certificate: %w", err)
	}
	temporaryCertificateInfo, err := requireRuntimeFile(runtimeDirectory, temporaryCertificate, publicFileMode)
	if err != nil {
		return Certificate{}, fmt.Errorf("validate issued management certificate: %w", err)
	}
	if contents, err := readRuntimeFile(runtimeDirectory, temporaryCertificate, publicFileMode); err != nil || len(contents) == 0 {
		return Certificate{}, fmt.Errorf("read issued management certificate")
	}
	inspection, err := i.runner.Run(ctx, Command{Path: store.sshKeygenPath, Args: []string{"-L", "-f", temporaryCertificate}})
	actual, valid := parseCertificateInspection(inspection.Stdout)
	if err != nil || inspection.Truncated || !valid || !certificateInspectionMatchesSigningBracket(actual, certificate, signingStarted, signingFinished) {
		return Certificate{}, fmt.Errorf("issued management certificate inspection failed")
	}
	if err := publishCertificate(runtimeDirectory, temporaryCertificate, temporaryCertificateInfo, certificate.Path, output, exists); err != nil {
		return Certificate{}, fmt.Errorf("publish management certificate: %w", err)
	}
	certificate.NotBefore, certificate.NotAfter = actual.NotBefore, actual.NotAfter
	return certificate, nil
}

func admitCertificateOutput(runtimeDirectory, path string) (os.FileInfo, bool, error) {
	if err := requirePrivateTree(runtimeDirectory, filepath.Dir(path)); err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := requireFileInfo(path, info, publicFileMode); err != nil {
		return nil, false, err
	}
	return info, true, nil
}

func makeTemporaryClientKey(runtimeDirectory, clientKeyPath string) (string, os.FileInfo, error) {
	contents, err := readRuntimeFile(runtimeDirectory, clientKeyPath, privateFileMode)
	if err != nil {
		return "", nil, err
	}
	directory := filepath.Dir(clientKeyPath)
	for range 4 {
		random := make([]byte, 16)
		if _, err := rand.Read(random); err != nil {
			return "", nil, err
		}
		path := filepath.Join(directory, ".boxwarden-cert-"+hex.EncodeToString(random))
		if _, err := os.Lstat(path + "-cert.pub"); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return "", nil, err
		}
		if err := writePrivateNew(path, contents); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return "", nil, err
		}
		info, err := requireRuntimeFile(runtimeDirectory, path, privateFileMode)
		if err != nil {
			if exact, check := requirePrivateFile(path); check == nil {
				_ = removeExactFile(path, exact, privateFileMode)
			}
			return "", nil, err
		}
		return path, info, nil
	}
	return "", nil, fmt.Errorf("temporary certificate input collisions")
}

func cleanupTemporaryCertificate(runtimeDirectory, path string) {
	info, err := requireRuntimeFile(runtimeDirectory, path, publicFileMode)
	if err == nil {
		_ = removeExactFile(path, info, publicFileMode)
	}
}

func publishCertificate(runtimeDirectory, temporary string, temporaryInfo os.FileInfo, destination string, destinationInfo os.FileInfo, destinationExists bool) error {
	currentTemporary, err := requireRuntimeFile(runtimeDirectory, temporary, publicFileMode)
	if err != nil || !os.SameFile(currentTemporary, temporaryInfo) {
		if err == nil {
			err = fmt.Errorf("temporary certificate changed before publication")
		}
		return err
	}
	current, exists, err := admitCertificateOutput(runtimeDirectory, destination)
	if err != nil {
		return err
	}
	if exists != destinationExists || (exists && !os.SameFile(current, destinationInfo)) {
		return fmt.Errorf("certificate destination changed before publication")
	}
	if err := os.Rename(temporary, destination); err != nil {
		return err
	}
	_, err = requireRuntimeFile(runtimeDirectory, destination, publicFileMode)
	return err
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
	if !certificateInspectionHasExpectedPolicy(actual, requested) {
		return false
	}
	return actual.NotBefore.Equal(requested.NotBefore.UTC()) && actual.NotAfter.Equal(requested.NotAfter.UTC())
}

func certificateInspectionMatchesSigningBracket(actual, requested Certificate, started, finished time.Time) bool {
	if !certificateInspectionHasExpectedPolicy(actual, requested) || finished.Before(started) {
		return false
	}
	// ssh-keygen serializes validity to whole seconds. The certificate's
	// reference instant is therefore represented by [reference, reference+1s),
	// which must overlap the trusted bracket around the signing subprocess.
	reference := actual.NotBefore.UTC().Add(renewalWindow)
	return !reference.After(finished.UTC()) && reference.Add(time.Second).After(started.UTC())
}

func certificateInspectionHasExpectedPolicy(actual, requested Certificate) bool {
	return actual.Identity == requested.Identity && actual.Principal == requested.Principal && actual.NotAfter.Sub(actual.NotBefore) == certificateLifetime+renewalWindow
}

// RenewalRequired is pure policy: certificates are refreshed at or inside the fixed five-minute window.
func RenewalRequired(certificate Certificate, now time.Time) bool {
	return !certificate.NotAfter.After(now.Add(renewalWindow))
}
