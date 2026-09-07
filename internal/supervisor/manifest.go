package supervisor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

const (
	requestName            = "supervisor-request.json"
	manifestName           = "supervisor-manifest.json"
	socketName             = "supervisor.sock"
	lockName               = "generation.lock"
	maxDiagnosticBytes     = 2048
	maxControlBytes        = 16 << 10
	maxEvidenceItems       = 8
	maxGenerationLockBytes = 512
)

type Binding struct{ Domain, SessionID, BackendKind, BackendObject, Generation string }

// generationLockRecord is a fixed, non-secret proof that generation.lock was
// published for this exact canonical immutable request. Its descriptor is held
// by each admitting owner; the record is never an authority by itself.
type generationLockRecord struct {
	Version       int     `json:"version"`
	Binding       Binding `json:"binding"`
	RequestSHA256 string  `json:"request_sha256"`
}

func canonicalLaunchRequestBytes(request LaunchRequest) ([]byte, error) {
	if err := validLaunchRequest(request); err != nil {
		return nil, err
	}
	return json.Marshal(request)
}

func boundLockRecord(request LaunchRequest) (generationLockRecord, error) {
	data, err := canonicalLaunchRequestBytes(request)
	if err != nil {
		return generationLockRecord{}, err
	}
	sum := sha256.Sum256(data)
	return generationLockRecord{Version: 1, Binding: request.Binding, RequestSHA256: fmt.Sprintf("%x", sum)}, nil
}

// writeBoundGenerationLock is deliberately narrow: callers cannot select an
// arbitrary payload or destination, only the fixed lock in this request's
// exact runtime root. First publication uses the staging-only helper below.
func writeBoundGenerationLock(path string, request LaunchRequest) error {
	if filepath.Base(path) != lockName || filepath.Dir(path) != request.RuntimeDirectory {
		return fmt.Errorf("generation lock path is not the fixed runtime lock path")
	}
	return writeBoundGenerationLockAt(path, request)
}

func writeBoundGenerationLockAt(path string, request LaunchRequest) error {
	if filepath.Base(path) != lockName || !privateDirectory(filepath.Dir(path)) {
		return fmt.Errorf("generation lock path is not an owner-private fixed path")
	}
	record, err := boundLockRecord(request)
	if err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > maxGenerationLockBytes {
		return fmt.Errorf("generation lock encoding exceeds bound")
	}
	return writePrivateFile(path, data)
}

// admitBoundGenerationLock returns a retained descriptor only after proving
// the exact fixed path, private regular-file identity, strict record encoding,
// binding, and canonical-request digest.
func admitBoundGenerationLock(path string, request LaunchRequest) (*retainedPrivateFile, error) {
	if filepath.Base(path) != lockName || filepath.Dir(path) != request.RuntimeDirectory {
		return nil, fmt.Errorf("generation lock path is not the fixed runtime lock path")
	}
	if !privateDirectory(request.RuntimeDirectory) {
		return nil, fmt.Errorf("generation lock runtime parent is not owner-private")
	}
	return admitBoundGenerationLockAt(path, request)
}

func admitBoundGenerationLockAt(path string, request LaunchRequest) (*retainedPrivateFile, error) {
	if filepath.Base(path) != lockName {
		return nil, fmt.Errorf("generation lock is not fixed")
	}
	retained, err := admitPrivateRegular(path)
	if err != nil {
		return nil, fmt.Errorf("generation lock is not an owner-private immutable file: %w", err)
	}
	return validateBoundGenerationLock(retained, request)
}

func validateBoundGenerationLock(retained *retainedPrivateFile, request LaunchRequest) (*retainedPrivateFile, error) {
	data, err := retained.read()
	if err != nil || len(data) > maxGenerationLockBytes {
		_ = retained.close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("generation lock encoding exceeds bound")
	}
	var record generationLockRecord
	if err := decodeExact(data, &record); err != nil {
		_ = retained.close()
		return nil, err
	}
	want, err := boundLockRecord(request)
	if err != nil || !reflect.DeepEqual(record, want) {
		_ = retained.close()
		return nil, fmt.Errorf("generation lock does not bind exact request")
	}
	return retained, nil
}

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
	Binding           Binding         `json:"binding"`
	RuntimeDirectory  string          `json:"runtime_directory"`
	HostConfigPath    string          `json:"host_config_path"`
	SessionRecordName string          `json:"session_record_name"`
	Host              HostExpectation `json:"host"`
	CA                CAExpectation   `json:"ca"`
}

// HostExpectation contains only comparable, non-secret runtime facts. It has
// no opaque Screen capability, descriptor, process, or executable authority.
type HostExpectation struct {
	Manifest      hostx.Manifest `json:"manifest"`
	ScreenPath    string         `json:"screen_path"`
	ScreenSHA256  string         `json:"screen_sha256"`
	ScreenVersion string         `json:"screen_version"`
	SoftnetBinDir string         `json:"softnet_bin_dir"`
}

// CAExpectation is the serializable public projection of one admitted CA.
type CAExpectation struct {
	Version      int    `json:"version"`
	Domain       string `json:"domain"`
	Algorithm    string `json:"algorithm"`
	PublicKey    string `json:"public_key"`
	PublicDigest string `json:"public_digest"`
	Fingerprint  string `json:"fingerprint"`
	CreationUUID string `json:"creation_uuid"`
	CreatorUID   int    `json:"creator_uid"`
	CreatorName  string `json:"creator_name"`
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

// retainedPrivateFile is a supervisor-owned capability for one admitted
// immutable artifact. Its descriptor prevents a removed inode from being
// reused under the same path before identity-checked cleanup completes.
type retainedPrivateFile struct {
	identity FileIdentity
	file     *os.File
}

// privateRegularAdmissionHook is a deterministic test seam for the narrow
// lstat/open/lstat admission boundary. Production leaves it nil.
var privateRegularAdmissionHook func()

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

// RuntimeStartEvidence records only exact direct children and endpoints that a
// held-capability runtime owner created.
type RuntimeStartEvidence struct {
	Children  []NamedProcessEvidence `json:"children"`
	Endpoints []NamedFileEvidence    `json:"endpoints"`
	Broker    BrokerEvidence         `json:"broker"`
}

// RuntimeStartResult makes ownership after Start explicit. On an error,
// Owned=false means the owner made no mutation (or completed its rollback),
// while Owned=true means the supervisor must retain and reap the owner before
// it may remove any generation namespace evidence. A successful Start always
// returns Owned=true with valid Evidence.
type RuntimeStartResult struct {
	Evidence RuntimeStartEvidence
	Owned    bool
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
	if !canonicalAbsolute(request.RuntimeDirectory) {
		return fmt.Errorf("runtime directory must be canonical and absolute")
	}
	if !canonicalAbsolute(request.HostConfigPath) {
		return fmt.Errorf("host config path must be canonical and absolute")
	}
	if !validPart(request.SessionRecordName) {
		return fmt.Errorf("canonical session record name is required")
	}
	if err := validHostExpectation(request.Host); err != nil {
		return err
	}
	if err := validCAExpectation(request.CA); err != nil {
		return err
	}
	if request.CA.Domain != request.Binding.Domain {
		return fmt.Errorf("CA expectation does not match supervisor domain")
	}
	return nil
}

func validHostExpectation(expectation HostExpectation) error {
	if err := expectation.Manifest.Validate(); err != nil {
		return fmt.Errorf("host manifest expectation is invalid: %w", err)
	}
	if !canonicalAbsolute(expectation.ScreenPath) || len(expectation.ScreenSHA256) != 64 || expectation.ScreenVersion == "" || !canonicalAbsolute(expectation.SoftnetBinDir) {
		return fmt.Errorf("host runtime expectation is invalid")
	}
	return nil
}

func validCAExpectation(expectation CAExpectation) error {
	if expectation.Version <= 0 || !validPart(expectation.Domain) || expectation.Algorithm != "ssh-ed25519" || expectation.PublicKey == "" || expectation.PublicDigest == "" || expectation.Fingerprint == "" || !validUUIDText(expectation.CreationUUID) || expectation.CreatorUID < 0 || expectation.CreatorName == "" {
		return fmt.Errorf("CA expectation is invalid")
	}
	return nil
}

func validUUIDText(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, r := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if r != '-' {
				return false
			}
			continue
		}
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}
func canonicalAbsolute(path string) bool {
	return path != "/" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\x00\r\n")
}
func privateDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 && info.Mode().Perm() == 0o700 && ownedByCurrentUser(info)
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

func admitPrivateRegular(path string) (*retainedPrivateFile, error) {
	expected, err := capturePrivateRegular(path)
	if err != nil {
		return nil, err
	}
	if privateRegularAdmissionHook != nil {
		privateRegularAdmissionHook()
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	actual, err := identityFor(path, info)
	if err != nil || !expected.matches(actual) || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || !ownedByCurrentUser(info) {
		_ = file.Close()
		return nil, fmt.Errorf("owner-private regular artifact changed during admission")
	}
	// This is the final pathname validation before decoding the held file. An
	// active same-UID replacement after this check remains outside this local
	// admission claim; retained cleanup will still preserve a changed path.
	postOpen, postInfo, err := captureIdentity(path)
	if err != nil || !actual.matches(postOpen) || postInfo.Mode()&os.ModeSymlink != 0 || !postInfo.Mode().IsRegular() || postInfo.Mode().Perm() != 0o600 || !ownedByCurrentUser(postInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("owner-private regular artifact changed during admission")
	}
	return &retainedPrivateFile{identity: actual, file: file}, nil
}

func (r *retainedPrivateFile) read() ([]byte, error) {
	if r == nil || r.file == nil {
		return nil, fmt.Errorf("retained artifact is unavailable")
	}
	if _, err := r.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(r.file, maxControlBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > maxControlBytes {
		return nil, fmt.Errorf("supervisor artifact exceeds bound")
	}
	return data, nil
}

func (r *retainedPrivateFile) close() error {
	if r == nil || r.file == nil {
		return nil
	}
	file := r.file
	r.file = nil
	return file.Close()
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
	return writeLaunchRequestAt(path, request)
}
func writeLaunchRequestAt(path string, request LaunchRequest) error {
	if filepath.Base(path) != requestName || !privateDirectory(filepath.Dir(path)) {
		return fmt.Errorf("request path is not an owner-private staging path")
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
	request, retained, err := admitLaunchRequest(path)
	if retained != nil {
		err = errors.Join(err, retained.close())
	}
	return request, err
}

func admitLaunchRequest(path string) (LaunchRequest, *retainedPrivateFile, error) {
	if filepath.Base(path) != requestName {
		return LaunchRequest{}, nil, fmt.Errorf("supervisor request is not an owner-private immutable file")
	}
	retained, err := admitPrivateRegular(path)
	if err != nil {
		return LaunchRequest{}, nil, fmt.Errorf("supervisor request is not an owner-private immutable file: %w", err)
	}
	data, err := retained.read()
	if err != nil {
		_ = retained.close()
		return LaunchRequest{}, nil, err
	}
	var request LaunchRequest
	if err := decodeExact(data, &request); err != nil {
		_ = retained.close()
		return LaunchRequest{}, nil, err
	}
	if err := validLaunchRequest(request); err != nil {
		_ = retained.close()
		return LaunchRequest{}, nil, err
	}
	if filepath.Dir(path) != request.RuntimeDirectory {
		_ = retained.close()
		return LaunchRequest{}, nil, fmt.Errorf("request is outside bound runtime directory")
	}
	return request, retained, nil
}
func writeManifest(path string, manifest Manifest) error {
	if filepath.Dir(path) != manifest.RuntimeDirectory || filepath.Base(path) != manifestName {
		return fmt.Errorf("manifest path is not the fixed runtime manifest path")
	}
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
	manifest, retained, err := admitManifest(path)
	if retained != nil {
		err = errors.Join(err, retained.close())
	}
	return manifest, err
}

func admitManifest(path string) (Manifest, *retainedPrivateFile, error) {
	if filepath.Base(path) != manifestName {
		return Manifest{}, nil, fmt.Errorf("supervisor manifest is not an owner-private immutable file")
	}
	retained, err := admitPrivateRegular(path)
	if err != nil {
		return Manifest{}, nil, fmt.Errorf("supervisor manifest is not an owner-private immutable file: %w", err)
	}
	data, err := retained.read()
	if err != nil {
		_ = retained.close()
		return Manifest{}, nil, err
	}
	var manifest Manifest
	if err := decodeExact(data, &manifest); err != nil {
		_ = retained.close()
		return Manifest{}, nil, err
	}
	if err := validManifest(manifest); err != nil {
		_ = retained.close()
		return Manifest{}, nil, err
	}
	if filepath.Dir(path) != manifest.RuntimeDirectory {
		_ = retained.close()
		return Manifest{}, nil, fmt.Errorf("manifest is outside declared runtime directory")
	}
	return manifest, retained, nil
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
		if (value.Role != "tart-serial" && value.Role != "operator-console") || !value.Identity.valid() || seen[value.Role] || value.Identity.Path != filepath.Join(runtime, "serial", value.Role) {
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
	duplicates := json.NewDecoder(strings.NewReader(string(data)))
	if err := rejectDuplicateJSONFields(duplicates); err != nil {
		return err
	}
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

func rejectDuplicateJSONFields(decoder *json.Decoder) error {
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return err
			}
			field, ok := name.(string)
			if !ok || seen[field] {
				return fmt.Errorf("duplicate JSON field %q", field)
			}
			seen[field] = true
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err := decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
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
