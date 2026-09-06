package supervisor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	requestName        = "supervisor-request.json"
	manifestName       = "supervisor-manifest.json"
	socketName         = "supervisor.sock"
	lockName           = "generation.lock"
	maxDiagnosticBytes = 2048
	maxControlBytes    = 16 << 10
	maxEvidenceItems   = 8
)

type Binding struct{ Domain, SessionID, BackendKind, BackendObject, Generation string }
type Snapshot struct {
	Binding                                                                                            Binding `json:"binding"`
	BackendRunning, BrokerHealthy, ScreenHealthy, PinPresent, CertificateCurrent, ProbeOK, ZoneMatches bool
	ObservedAt                                                                                         time.Time `json:"observed_at"`
	Diagnostic                                                                                         string    `json:"diagnostic"`
}
type Controller interface {
	Snapshot(context.Context, Binding) (Snapshot, error)
	Stop(context.Context, Binding) error
}
type LaunchRequest struct {
	Binding          Binding `json:"binding"`
	RuntimeDirectory string  `json:"runtime_directory"`
	HostConfigPath   string  `json:"host_config_path"`
}
type Launcher interface {
	Launch(context.Context, LaunchRequest) error
}

// ProcessIdentity is observation only. It is never a PID control capability.
type ProcessIdentity struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Unique    uint64    `json:"unique"`
}

func (p ProcessIdentity) valid() bool { return p.PID > 0 && p.Unique != 0 && !p.StartedAt.IsZero() }
func (p ProcessIdentity) matches(other ProcessIdentity) bool {
	return p.PID == other.PID && p.Unique == other.Unique && p.StartedAt.Equal(other.StartedAt)
}

// FileIdentity is comparison evidence, never an authority to adopt or delete.
type FileIdentity struct {
	Path   string `json:"path"`
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
}

func (f FileIdentity) valid() bool { return canonicalAbsolute(f.Path) && f.Device != 0 && f.Inode != 0 }
func (f FileIdentity) matches(other FileIdentity) bool {
	return f.Path == other.Path && f.Device == other.Device && f.Inode == other.Inode
}

type NamedProcessEvidence struct {
	Role     string          `json:"role"`
	Identity ProcessIdentity `json:"identity"`
}
type NamedFileEvidence struct {
	Role     string       `json:"role"`
	Identity FileIdentity `json:"identity"`
}
type BrokerEvidence struct {
	Healthy  bool `json:"healthy"`
	Poisoned bool `json:"poisoned"`
}

// RuntimeStartEvidence is returned by the held-capability runtime owner. It
// records only exact direct children and endpoints that it created.
type RuntimeStartEvidence struct {
	Children  []NamedProcessEvidence `json:"children"`
	Endpoints []NamedFileEvidence    `json:"endpoints"`
	Broker    BrokerEvidence         `json:"broker"`
}
type RuntimeEvidence struct {
	Supervisor       ProcessIdentity        `json:"supervisor"`
	Children         []NamedProcessEvidence `json:"children"`
	Endpoints        []NamedFileEvidence    `json:"endpoints"`
	RuntimeDirectory FileIdentity           `json:"runtime_directory"`
	Broker           BrokerEvidence         `json:"broker"`
}
type Manifest struct {
	Version          int             `json:"version"`
	Binding          Binding         `json:"binding"`
	RuntimeDirectory string          `json:"runtime_directory"`
	SocketPath       string          `json:"socket_path"`
	ControlKey       string          `json:"control_key"`
	Evidence         RuntimeEvidence `json:"evidence"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (b Binding) valid() bool {
	return validPart(b.Domain) && validPart(b.SessionID) && validPart(b.BackendKind) && validPart(b.BackendObject) && validPart(b.Generation)
}
func validPart(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func validLaunchRequest(request LaunchRequest) error {
	if !request.Binding.valid() {
		return fmt.Errorf("invalid supervisor binding")
	}
	if !privateDirectory(request.RuntimeDirectory) {
		return fmt.Errorf("runtime directory must be canonical owner-private directory")
	}
	if !canonicalAbsolute(request.HostConfigPath) {
		return fmt.Errorf("host config path must be canonical and absolute")
	}
	return nil
}
func canonicalAbsolute(path string) bool {
	return path != "/" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}
func privateDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
}
func privateRegular(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o600 && ownedByCurrentUser(info)
}
func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
func identityFor(path string, info os.FileInfo) (FileIdentity, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Dev == 0 || stat.Ino == 0 {
		return FileIdentity{}, fmt.Errorf("file identity unavailable")
	}
	return FileIdentity{Path: path, Device: uint64(stat.Dev), Inode: uint64(stat.Ino)}, nil
}
func captureIdentity(path string) (FileIdentity, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return FileIdentity{}, nil, err
	}
	identity, err := identityFor(path, info)
	return identity, info, err
}
func capturePrivateRegular(path string) (FileIdentity, error) {
	identity, info, err := captureIdentity(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		return FileIdentity{}, fmt.Errorf("owner-private regular identity unavailable")
	}
	return identity, nil
}
func capturePrivateDirectory(path string) (FileIdentity, error) {
	identity, info, err := captureIdentity(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 || !ownedByCurrentUser(info) {
		return FileIdentity{}, fmt.Errorf("owner-private directory identity unavailable")
	}
	return identity, nil
}
func identityStillMatches(identity FileIdentity, check func(os.FileInfo) bool) error {
	current, info, err := captureIdentity(identity.Path)
	if err != nil {
		return err
	}
	if !identity.matches(current) || !check(info) {
		return fmt.Errorf("runtime entry was replaced")
	}
	return nil
}

func writeLaunchRequest(path string, request LaunchRequest) error {
	if err := validLaunchRequest(request); err != nil {
		return err
	}
	if filepath.Dir(path) != request.RuntimeDirectory || filepath.Base(path) != requestName {
		return fmt.Errorf("request path is not the fixed runtime request path")
	}
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}
func writePrivateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create owner-private immutable file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func readLaunchRequest(path string) (LaunchRequest, error) {
	if filepath.Base(path) != requestName || !privateRegular(path) {
		return LaunchRequest{}, fmt.Errorf("supervisor request is not an owner-private immutable file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LaunchRequest{}, err
	}
	if len(data) == 0 || len(data) > maxControlBytes {
		return LaunchRequest{}, fmt.Errorf("supervisor request exceeds bound")
	}
	var request LaunchRequest
	if err := decodeExact(data, &request); err != nil {
		return LaunchRequest{}, err
	}
	if err := validLaunchRequest(request); err != nil {
		return LaunchRequest{}, err
	}
	if filepath.Dir(path) != request.RuntimeDirectory {
		return LaunchRequest{}, fmt.Errorf("request is outside bound runtime directory")
	}
	return request, nil
}
func writeManifest(path string, manifest Manifest) error {
	if err := validManifest(manifest); err != nil {
		return err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	return writePrivateFile(path, data)
}
func readManifest(path string) (Manifest, error) {
	if filepath.Base(path) != manifestName || !privateRegular(path) {
		return Manifest{}, fmt.Errorf("supervisor manifest is not an owner-private immutable file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	if len(data) == 0 || len(data) > maxControlBytes {
		return Manifest{}, fmt.Errorf("supervisor manifest exceeds bound")
	}
	var manifest Manifest
	if err := decodeExact(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if err := validManifest(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}
func validStartEvidence(evidence RuntimeStartEvidence, runtime string) error {
	if len(evidence.Children) != 2 || len(evidence.Endpoints) != 2 || len(evidence.Children) > maxEvidenceItems || len(evidence.Endpoints) > maxEvidenceItems || evidence.Broker.Healthy == evidence.Broker.Poisoned {
		return fmt.Errorf("invalid runtime start evidence")
	}
	if err := validProcessRoles(evidence.Children); err != nil {
		return err
	}
	return validFileRoles(evidence.Endpoints, runtime)
}
func validProcessRoles(values []NamedProcessEvidence) error {
	seen := map[string]bool{}
	for _, value := range values {
		if (value.Role != "backend" && value.Role != "screen") || !value.Identity.valid() || seen[value.Role] {
			return fmt.Errorf("invalid direct child evidence")
		}
		seen[value.Role] = true
	}
	if !seen["backend"] || !seen["screen"] {
		return fmt.Errorf("missing direct child evidence")
	}
	return nil
}
func validFileRoles(values []NamedFileEvidence, runtime string) error {
	seen := map[string]bool{}
	for _, value := range values {
		if (value.Role != "tart-serial" && value.Role != "operator-console") || !value.Identity.valid() || seen[value.Role] || value.Identity.Path != filepath.Join(runtime, value.Role) {
			return fmt.Errorf("invalid endpoint evidence")
		}
		if err := identityStillMatches(value.Identity, func(info os.FileInfo) bool {
			return info.Mode()&os.ModeSymlink != 0 && ownedByCurrentUser(info)
		}); err != nil {
			return fmt.Errorf("endpoint evidence: %w", err)
		}
		seen[value.Role] = true
	}
	if !seen["tart-serial"] || !seen["operator-console"] {
		return fmt.Errorf("missing endpoint evidence")
	}
	return nil
}
func validEvidence(evidence RuntimeEvidence, runtime string) error {
	if !evidence.Supervisor.valid() || evidence.RuntimeDirectory.Path != runtime || !evidence.RuntimeDirectory.valid() || evidence.Broker.Healthy == evidence.Broker.Poisoned {
		return fmt.Errorf("invalid supervisor runtime evidence")
	}
	if err := validStartEvidence(RuntimeStartEvidence{Children: evidence.Children, Endpoints: evidence.Endpoints, Broker: evidence.Broker}, runtime); err != nil {
		return err
	}
	if err := identityStillMatches(evidence.RuntimeDirectory, func(info os.FileInfo) bool {
		return info.IsDir() && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
	}); err != nil {
		return fmt.Errorf("runtime directory evidence: %w", err)
	}
	return nil
}
func validManifest(manifest Manifest) error {
	if manifest.Version != 1 || !manifest.Binding.valid() || !privateDirectory(manifest.RuntimeDirectory) || !canonicalAbsolute(manifest.SocketPath) || filepath.Dir(manifest.SocketPath) != manifest.RuntimeDirectory || filepath.Base(manifest.SocketPath) != socketName || manifest.CreatedAt.IsZero() {
		return fmt.Errorf("invalid supervisor manifest")
	}
	if _, err := decodeKey(manifest.ControlKey); err != nil {
		return fmt.Errorf("invalid control key: %w", err)
	}
	return validEvidence(manifest.Evidence, manifest.RuntimeDirectory)
}
func decodeExact(data []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}
func newControlKey() ([]byte, string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return nil, "", err
	}
	return value, base64.RawStdEncoding.EncodeToString(value), nil
}
func decodeKey(value string) ([]byte, error) {
	key, err := base64.RawStdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("invalid key")
	}
	return key, nil
}
func newChallenge() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(value), nil
}
func snapshotReady(snapshot Snapshot) bool {
	return snapshot.BackendRunning && snapshot.BrokerHealthy && snapshot.ScreenHealthy && snapshot.PinPresent && snapshot.CertificateCurrent && snapshot.ProbeOK && snapshot.ZoneMatches
}
