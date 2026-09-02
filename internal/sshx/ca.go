package sshx

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const caMetadataVersion = 1

// ErrCAMissing reports that the selected domain has no CA material. Callers
// can distinguish an uninitialized domain from partial or unsafe CA state.
var ErrCAMissing = errors.New("management CA is missing")

// Command is one bounded argv-only trusted-host program invocation.
type Command struct {
	Path  string
	Args  []string
	Stdin []byte
}
type Result struct {
	Stdout    string
	Stderr    string
	Truncated bool
}
type Runner interface {
	Run(context.Context, Command) (Result, error)
}

type Identity interface {
	Current(context.Context) (Operator, error)
}
type Operator struct {
	UID  int    `json:"uid"`
	Name string `json:"name"`
}
type StaticIdentity struct {
	UID  int
	Name string
}

func (i StaticIdentity) Current(context.Context) (Operator, error) {
	return Operator{UID: i.UID, Name: i.Name}, nil
}

// Domain is the deliberately small configuration adapter consumed by sshx.
type Domain struct {
	ID        domain.ID
	StateRoot string
}

type CAStoreOptions struct {
	Runner        Runner
	Identity      Identity
	NewUUID       func() (string, error)
	SSHKeygenPath string
}
type CAStore struct {
	runner        Runner
	identity      Identity
	newUUID       func() (string, error)
	sshKeygenPath string
}

type CAIdentity struct {
	Version        int       `json:"version"`
	Domain         domain.ID `json:"domain"`
	Algorithm      string    `json:"algorithm"`
	PublicKey      string    `json:"public_key"`
	PublicDigest   string    `json:"public_digest"`
	Fingerprint    string    `json:"fingerprint"`
	CreationUUID   string    `json:"creation_uuid"`
	CreatorUID     int       `json:"creator_uid"`
	CreatorName    string    `json:"creator_name"`
	PrivateKeyPath string    `json:"-"`
	PublicKeyPath  string    `json:"-"`
	StateRoot      string    `json:"-"`
	SSHKeygenPath  string    `json:"-"`
}

func NewCAStore(options CAStoreOptions) *CAStore {
	path := options.SSHKeygenPath
	if path == "" {
		path = "/usr/bin/ssh-keygen"
	}
	return &CAStore{runner: options.Runner, identity: options.Identity, newUUID: options.NewUUID, sshKeygenPath: path}
}

func (s *CAStore) Init(ctx context.Context, current Domain, configured []Domain) (CAIdentity, error) {
	if err := validDomain(current); err != nil {
		return CAIdentity{}, err
	}
	if s.runner == nil || s.identity == nil || s.newUUID == nil {
		return CAIdentity{}, fmt.Errorf("CA store dependencies are required")
	}
	existing, err := s.Check(ctx, current, configured)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrCAMissing) {
		return CAIdentity{}, err
	}
	operator, err := s.identity.Current(ctx)
	if err != nil {
		return CAIdentity{}, fmt.Errorf("resolve creating operator: %w", err)
	}
	if operator.UID < 0 || operator.Name == "" {
		return CAIdentity{}, fmt.Errorf("creating operator is invalid")
	}
	creationUUID, err := s.newUUID()
	if err != nil {
		return CAIdentity{}, fmt.Errorf("generate CA creation UUID: %w", err)
	}
	if !validUUID(creationUUID) {
		return CAIdentity{}, fmt.Errorf("creation UUID is invalid")
	}
	if err := ensurePrivateDirectory(current.StateRoot); err != nil {
		return CAIdentity{}, fmt.Errorf("state root: %w", err)
	}
	dir := caDirectory(current.StateRoot)
	state, err := caState(dir)
	if err != nil {
		return CAIdentity{}, err
	}
	if state.complete {
		return s.Check(ctx, current, configured)
	}
	if state.any {
		return CAIdentity{}, fmt.Errorf("management CA state is partial; explicit manual remediation is required")
	}
	if err := ensurePrivateDirectory(filepath.Join(current.StateRoot, "identity")); err != nil {
		return CAIdentity{}, err
	}
	if err := ensurePrivateDirectory(dir); err != nil {
		return CAIdentity{}, err
	}
	privatePath := filepath.Join(dir, "ca")
	result, err := s.runner.Run(ctx, Command{Path: s.sshKeygenPath, Args: []string{"-q", "-t", "ed25519", "-f", privatePath, "-N", "", "-C", "boxwarden:" + string(current.ID) + ":management-ca"}})
	if err != nil {
		return CAIdentity{}, fmt.Errorf("create management CA: %w", err)
	}
	if result.Truncated {
		return CAIdentity{}, fmt.Errorf("create management CA produced oversized output")
	}
	if err := normalizePublicFile(filepath.Join(dir, "ca.pub")); err != nil {
		return CAIdentity{}, fmt.Errorf("normalize CA public key: %w", err)
	}
	identity, err := s.buildIdentity(ctx, current)
	if err != nil {
		return CAIdentity{}, err
	}
	identity.CreationUUID = creationUUID
	identity.CreatorUID, identity.CreatorName = operator.UID, operator.Name
	encoded, err := json.Marshal(identity)
	if err != nil {
		return CAIdentity{}, err
	}
	metadataPath := filepath.Join(dir, "metadata.json")
	if err := writePrivateNew(metadataPath, append(encoded, '\n')); err != nil {
		return CAIdentity{}, fmt.Errorf("write CA metadata: %w", err)
	}
	return s.Check(ctx, current, configured)
}

// Check validates the selected domain's existing CA without creating or
// repairing state. A completely absent CA returns ErrCAMissing; any partial,
// malformed, or unsafe state fails closed. Complete configured domains are
// checked for CA fingerprint reuse without searching outside that set.
func (s *CAStore) Check(ctx context.Context, current Domain, configured []Domain) (CAIdentity, error) {
	if err := validDomain(current); err != nil {
		return CAIdentity{}, err
	}
	return s.checkConfigured(ctx, current, configured)
}

func (s *CAStore) Load(ctx context.Context, current Domain) (CAIdentity, error) {
	if err := validDomain(current); err != nil {
		return CAIdentity{}, err
	}
	dir := caDirectory(current.StateRoot)
	if err := requirePrivateTree(current.StateRoot, dir); err != nil {
		return CAIdentity{}, fmt.Errorf("CA directory: %w", err)
	}
	state, err := caState(dir)
	if err != nil {
		return CAIdentity{}, err
	}
	if !state.complete {
		return CAIdentity{}, fmt.Errorf("management CA is missing or partial")
	}
	contents, err := readPrivateFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return CAIdentity{}, fmt.Errorf("read CA metadata: %w", err)
	}
	metadata, err := decodeCAIdentity(contents)
	if err != nil {
		return CAIdentity{}, fmt.Errorf("parse CA metadata: %w", err)
	}
	actual, err := s.buildIdentity(ctx, current)
	if err != nil {
		return CAIdentity{}, err
	}
	if metadata.Version != caMetadataVersion || metadata.Domain != current.ID || metadata.Algorithm != "ssh-ed25519" || !validUUID(metadata.CreationUUID) || metadata.CreatorUID < 0 || metadata.CreatorName == "" {
		return CAIdentity{}, fmt.Errorf("CA metadata is invalid")
	}
	if metadata.PublicKey != actual.PublicKey || metadata.PublicDigest != actual.PublicDigest || metadata.Fingerprint != actual.Fingerprint {
		return CAIdentity{}, fmt.Errorf("CA metadata does not match key material")
	}
	if s.identity == nil {
		return CAIdentity{}, fmt.Errorf("CA identity verifier is required")
	}
	operator, err := s.identity.Current(ctx)
	if err != nil || operator.UID != metadata.CreatorUID || operator.Name != metadata.CreatorName {
		return CAIdentity{}, fmt.Errorf("CA creating operator no longer matches metadata")
	}
	actual.CreationUUID, actual.CreatorUID, actual.CreatorName, actual.StateRoot, actual.SSHKeygenPath = metadata.CreationUUID, metadata.CreatorUID, metadata.CreatorName, current.StateRoot, s.sshKeygenPath
	return actual, nil
}

func (s *CAStore) buildIdentity(ctx context.Context, current Domain) (CAIdentity, error) {
	dir := caDirectory(current.StateRoot)
	privatePath, publicPath := filepath.Join(dir, "ca"), filepath.Join(dir, "ca.pub")
	if _, err := readPrivateFile(privatePath); err != nil {
		return CAIdentity{}, fmt.Errorf("inspect CA private key: %w", err)
	}
	publicBytes, err := readPublicFile(publicPath)
	if err != nil {
		return CAIdentity{}, fmt.Errorf("inspect CA public key: %w", err)
	}
	public, digest, fingerprint, err := parseEd25519PublicKey(string(publicBytes))
	if err != nil {
		return CAIdentity{}, fmt.Errorf("parse CA public key: %w", err)
	}
	if s.runner == nil {
		return CAIdentity{}, fmt.Errorf("CA runner is required")
	}
	derived, err := s.runner.Run(ctx, Command{Path: s.sshKeygenPath, Args: []string{"-y", "-f", privatePath}})
	if err != nil || derived.Truncated {
		return CAIdentity{}, fmt.Errorf("derive CA public key: %w", err)
	}
	derivedPublic, _, _, err := parseEd25519PublicKey(derived.Stdout)
	if err != nil || derivedPublic != public {
		return CAIdentity{}, fmt.Errorf("CA private/public key mismatch")
	}
	return CAIdentity{Version: caMetadataVersion, Domain: current.ID, Algorithm: "ssh-ed25519", PublicKey: public, PublicDigest: digest, Fingerprint: fingerprint, PrivateKeyPath: privatePath, PublicKeyPath: publicPath, StateRoot: current.StateRoot, SSHKeygenPath: s.sshKeygenPath}, nil
}

func decodeCAIdentity(contents []byte) (CAIdentity, error) {
	fields, err := decodeExactObject(contents, "version", "domain", "algorithm", "public_key", "public_digest", "fingerprint", "creation_uuid", "creator_uid", "creator_name")
	if err != nil {
		return CAIdentity{}, err
	}
	var value CAIdentity
	var rawDomain string
	for name, target := range map[string]any{
		"version": &value.Version, "domain": &rawDomain, "algorithm": &value.Algorithm, "public_key": &value.PublicKey,
		"public_digest": &value.PublicDigest, "fingerprint": &value.Fingerprint, "creation_uuid": &value.CreationUUID,
		"creator_uid": &value.CreatorUID, "creator_name": &value.CreatorName,
	} {
		if err := decodeField(fields, name, target); err != nil {
			return CAIdentity{}, err
		}
	}
	value.Domain = domain.ID(rawDomain)
	return value, nil
}

// checkConfigured validates every supplied configured domain before returning
// the selected identity. It never discovers or falls back to an undeclared
// domain. ErrCAMissing is returned only after every configured root has been
// checked and no configured CA state is partial, unsafe, or duplicated.
func (s *CAStore) checkConfigured(ctx context.Context, current Domain, configured []Domain) (CAIdentity, error) {
	seenDomains := make(map[domain.ID]string, len(configured))
	fingerprints := make(map[string]domain.ID, len(configured))
	var selected CAIdentity
	selectedComplete := false
	selectedConfigured := false
	for _, candidate := range configured {
		if err := validDomain(candidate); err != nil {
			return CAIdentity{}, fmt.Errorf("configured domain: %w", err)
		}
		if root, found := seenDomains[candidate.ID]; found {
			return CAIdentity{}, fmt.Errorf("configured domain %q is declared more than once (%q and %q)", candidate.ID, root, candidate.StateRoot)
		}
		seenDomains[candidate.ID] = candidate.StateRoot
		if candidate.ID == current.ID {
			if candidate.StateRoot != current.StateRoot {
				return CAIdentity{}, fmt.Errorf("configured selected domain %q state root does not match", current.ID)
			}
			selectedConfigured = true
		}
		state, err := checkCAState(candidate)
		if err != nil {
			return CAIdentity{}, fmt.Errorf("inspect configured domain %q: %w", candidate.ID, err)
		}
		if !state.any {
			continue
		}
		if !state.complete {
			return CAIdentity{}, fmt.Errorf("configured domain %q has partial CA state", candidate.ID)
		}
		identity, err := s.Load(ctx, candidate)
		if err != nil {
			return CAIdentity{}, fmt.Errorf("load configured domain %q: %w", candidate.ID, err)
		}
		if other, found := fingerprints[identity.Fingerprint]; found {
			return CAIdentity{}, fmt.Errorf("management CA fingerprint is reused by configured domains %q and %q", other, candidate.ID)
		}
		fingerprints[identity.Fingerprint] = candidate.ID
		if candidate.ID == current.ID {
			selected, selectedComplete = identity, true
		}
	}
	if !selectedConfigured {
		return CAIdentity{}, fmt.Errorf("configured domains do not contain selected domain %q", current.ID)
	}
	if !selectedComplete {
		return CAIdentity{}, fmt.Errorf("selected domain %q: %w", current.ID, ErrCAMissing)
	}
	return selected, nil
}

type caStateResult struct {
	any      bool
	complete bool
}

// checkCAState establishes that the domain-root ancestry is safe before
// treating missing CA material as an uninitialized state. It performs only
// observations and never creates missing directories.
func checkCAState(current Domain) (caStateResult, error) {
	rootInfo, err := os.Lstat(current.StateRoot)
	if os.IsNotExist(err) {
		return caStateResult{}, nil
	}
	if err != nil {
		return caStateResult{}, err
	}
	if err := requirePrivateDirectoryInfo(rootInfo); err != nil {
		return caStateResult{}, fmt.Errorf("CA state root: %w", err)
	}
	identityRoot := filepath.Join(current.StateRoot, "identity")
	identityInfo, err := os.Lstat(identityRoot)
	if os.IsNotExist(err) {
		return caStateResult{}, nil
	}
	if err != nil {
		return caStateResult{}, err
	}
	if err := requirePrivateDirectoryInfo(identityInfo); err != nil {
		return caStateResult{}, fmt.Errorf("CA identity directory: %w", err)
	}
	return caState(caDirectory(current.StateRoot))
}

func caState(dir string) (caStateResult, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return caStateResult{}, nil
	}
	if err != nil {
		return caStateResult{}, err
	}
	if err := requirePrivateDirectoryInfo(info); err != nil {
		return caStateResult{}, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return caStateResult{}, err
	}
	allowed := map[string]bool{"ca": true, "ca.pub": true, "metadata.json": true}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return caStateResult{}, fmt.Errorf("unexpected CA state entry %q", entry.Name())
		}
	}
	present := 0
	for _, name := range []string{"ca", "ca.pub", "metadata.json"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			present++
		} else if !os.IsNotExist(err) {
			return caStateResult{}, err
		}
	}
	return caStateResult{any: present > 0, complete: present == 3}, nil
}

func validDomain(value Domain) error {
	if _, err := domain.Parse(string(value.ID)); err != nil {
		return err
	}
	if !filepath.IsAbs(value.StateRoot) || filepath.Clean(value.StateRoot) != value.StateRoot {
		return fmt.Errorf("domain state root must be canonical and absolute")
	}
	return nil
}

func parseEd25519PublicKey(raw string) (string, string, string, error) {
	if len(raw) == 0 || len(raw) > 1024 || strings.Contains(raw, "\r") {
		return "", "", "", fmt.Errorf("public key line is invalid")
	}
	raw = strings.TrimSuffix(raw, "\n")
	if raw == "" || strings.Contains(raw, "\n") {
		return "", "", "", fmt.Errorf("public key must be exactly one line")
	}
	fields := strings.Fields(raw)
	if (len(fields) != 2 && len(fields) != 3) || fields[0] != "ssh-ed25519" {
		return "", "", "", fmt.Errorf("must be one ssh-ed25519 public key")
	}
	decoded, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || !validEd25519WireKey(decoded) {
		return "", "", "", fmt.Errorf("public key encoding is invalid")
	}
	digest := sha256.Sum256(decoded)
	return fields[0] + " " + fields[1], hex.EncodeToString(digest[:]), "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:]), nil
}

func validEd25519WireKey(decoded []byte) bool {
	const algorithm = "ssh-ed25519"
	if len(decoded) != 4+len(algorithm)+4+32 || binary.BigEndian.Uint32(decoded[:4]) != uint32(len(algorithm)) || string(decoded[4:4+len(algorithm)]) != algorithm {
		return false
	}
	start := 4 + len(algorithm)
	return binary.BigEndian.Uint32(decoded[start:start+4]) == 32
}

func writePrivateNew(path string, contents []byte) error {
	root, err := openVerifiedRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := openNoFollow(root, filepath.Base(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Chmod(privateFileMode); err != nil {
		file.Close()
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	if err := requireFileInfo(info, privateFileMode); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i, value := range raw {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if value != '-' {
				return false
			}
			continue
		}
		if !(value >= '0' && value <= '9') && !(value >= 'a' && value <= 'f') {
			return false
		}
	}
	return true
}
