package sshx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	sshPath                    = "/usr/bin/ssh"
	guestManagementHelper      = "/usr/local/libexec/boxwarden-guest-bootstrap"
	maxManagementRequestBytes  = 64 << 10
	maxManagementResponseBytes = 64 << 10
	managementWallTimeout      = 30 * time.Second
)

type Connection struct {
	Address          string
	Port             uint16
	Binding          Binding
	Pin              HostKeyPin
	RuntimeDirectory string
	IdentityFile     string
	CertificateFile  string
	KnownHostsFile   string
}

type Client struct{ runner Runner }

func NewClient(runner Runner) *Client { return &Client{runner: runner} }

type WorkspaceMount struct {
	VolumeID       string `json:"volume_id"`
	FilesystemUUID string `json:"filesystem_uuid"`
	MountPath      string `json:"mount_path"`
}
type ProbeRequest struct{ Workspaces []WorkspaceMount }
type ProbeResult struct {
	OK bool `json:"ok"`
}
type ApplyZoneRequest struct{ Zone string }
type ReadZoneRequest struct{}
type PackageVersion struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
type GuestIdentity struct {
	MachineID string `json:"machine_id"`
	Hostname  string `json:"hostname"`
}

// managementRequest is intentionally package-private: callers choose only a concrete typed method.
type managementRequest struct {
	Version       int              `json:"version"`
	Kind          string           `json:"kind"`
	Domain        string           `json:"domain"`
	SessionID     string           `json:"session_id"`
	BackendKind   string           `json:"backend_kind"`
	BackendObject string           `json:"backend_object"`
	Zone          string           `json:"zone,omitempty"`
	Packages      []string         `json:"packages,omitempty"`
	Workspaces    []WorkspaceMount `json:"workspaces,omitempty"`
}

func (c *Client) Probe(ctx context.Context, connection Connection, probe ProbeRequest) (ProbeResult, error) {
	if err := validateWorkspaceMounts(probe.Workspaces); err != nil {
		return ProbeResult{}, err
	}
	request := managementRequestFor(connection.Binding, "probe", "")
	request.Workspaces = append([]WorkspaceMount(nil), probe.Workspaces...)
	output, err := c.run(ctx, connection, request)
	if err != nil {
		return ProbeResult{}, err
	}
	result, err := decodeProbeResult(output)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("parse probe response: %w", err)
	}
	return result, nil
}

// RequestShutdown asks the exact pinned guest helper to enqueue its fixed
// poweroff operation. The reply is not proof that the guest has stopped or
// that attached filesystems are clean; the retained VM handle proves exit.
func (c *Client) RequestShutdown(ctx context.Context, connection Connection) error {
	output, err := c.run(ctx, connection, managementRequestFor(connection.Binding, "request_shutdown", ""))
	if err != nil {
		return err
	}
	result, err := decodeProbeResult(output)
	if err != nil {
		return fmt.Errorf("parse shutdown acceptance: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("guest did not accept fixed shutdown")
	}
	return nil
}

// EnsureWorkspaces sends only exact UUID/path bindings to the fixed pinned
// guest helper. The guest validates the same schema before mounting.
func (c *Client) EnsureWorkspaces(ctx context.Context, connection Connection, mounts []WorkspaceMount) error {
	if len(mounts) == 0 {
		return fmt.Errorf("workspace mount request is empty")
	}
	if err := validateWorkspaceMounts(mounts); err != nil {
		return err
	}
	request := managementRequestFor(connection.Binding, "ensure_workspaces", "")
	request.Workspaces = append([]WorkspaceMount(nil), mounts...)
	output, err := c.run(ctx, connection, request)
	if err != nil {
		return err
	}
	result, err := decodeProbeResult(output)
	if err != nil {
		return fmt.Errorf("parse workspace mount response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("guest rejected exact workspace mount request")
	}
	return nil
}

func validateWorkspaceMounts(mounts []WorkspaceMount) error {
	if len(mounts) > 4 {
		return fmt.Errorf("workspace mount count exceeds four")
	}
	volumes, uuids, paths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, mount := range mounts {
		if !validUUID(mount.VolumeID) || !validUUID(mount.FilesystemUUID) || !validWorkspaceMountPath(mount.MountPath) || volumes[mount.VolumeID] || uuids[mount.FilesystemUUID] || paths[mount.MountPath] {
			return fmt.Errorf("invalid or duplicate workspace mount binding")
		}
		volumes[mount.VolumeID], uuids[mount.FilesystemUUID], paths[mount.MountPath] = true, true, true
	}
	return nil
}

func validWorkspaceMountPath(path string) bool {
	const prefix = "/home/boxwarden/workspaces/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	name := strings.TrimPrefix(path, prefix)
	if len(name) == 0 || len(name) > 63 || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !(name[i] >= 'a' && name[i] <= 'z' || name[i] >= '0' && name[i] <= '9') {
			return false
		}
	}
	return true
}

func (c *Client) ApplyZone(ctx context.Context, connection Connection, request ApplyZoneRequest) error {
	if !validZone(request.Zone) {
		return fmt.Errorf("invalid time zone")
	}
	output, err := c.run(ctx, connection, managementRequestFor(connection.Binding, "apply_zone", request.Zone))
	if err != nil {
		return err
	}
	result, err := decodeProbeResult(output)
	if err != nil {
		return fmt.Errorf("parse apply-zone response: %w", err)
	}
	if !result.OK {
		return fmt.Errorf("guest rejected time-zone application")
	}
	return nil
}

func (c *Client) ReadZone(ctx context.Context, connection Connection, _ ReadZoneRequest) (string, error) {
	output, err := c.run(ctx, connection, managementRequestFor(connection.Binding, "read_zone", ""))
	if err != nil {
		return "", err
	}
	zone, err := decodeZoneResult(output)
	if err != nil {
		return "", fmt.Errorf("parse time-zone response: %w", err)
	}
	if !validZone(zone) {
		return "", fmt.Errorf("received invalid time zone")
	}
	return zone, nil
}

// InspectPackages asks the pinned exact-generation management helper to query
// only named Debian packages. The result is bounded guest software evidence.
func (c *Client) InspectPackages(ctx context.Context, connection Connection, names []string) ([]PackageVersion, error) {
	if len(names) == 0 || len(names) > 128 {
		return nil, fmt.Errorf("package inspection count is invalid")
	}
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !validPackageName(name) || seen[name] {
			return nil, fmt.Errorf("invalid or duplicate package inspection name")
		}
		seen[name] = true
	}
	request := managementRequestFor(connection.Binding, "inspect_packages", "")
	request.Packages = append([]string(nil), names...)
	output, err := c.run(ctx, connection, request)
	if err != nil {
		return nil, err
	}
	fields, err := decodeExactObject(output, "version", "packages")
	if err != nil {
		return nil, fmt.Errorf("parse package inspection: %w", err)
	}
	var version int
	var raw []json.RawMessage
	if err := decodeField(fields, "version", &version); err != nil {
		return nil, err
	}
	if err := decodeField(fields, "packages", &raw); err != nil {
		return nil, err
	}
	if version != 1 || len(raw) != len(names) {
		return nil, fmt.Errorf("package inspection version or count mismatch")
	}
	result := make([]PackageVersion, 0, len(raw))
	for index, item := range raw {
		entry, err := decodeExactObject(item, "name", "version")
		if err != nil {
			return nil, fmt.Errorf("package inspection entry: %w", err)
		}
		var pkg PackageVersion
		if err := decodeField(entry, "name", &pkg.Name); err != nil {
			return nil, err
		}
		if err := decodeField(entry, "version", &pkg.Version); err != nil {
			return nil, err
		}
		if pkg.Name != names[index] || !validPackageVersion(pkg.Version) {
			return nil, fmt.Errorf("package inspection does not match exact request")
		}
		result = append(result, pkg)
	}
	return result, nil
}

// InspectIdentity requests only the fixed clone identity check from the
// pinned management helper. The response is guest diagnostic evidence.
func (c *Client) InspectIdentity(ctx context.Context, connection Connection) (GuestIdentity, error) {
	output, err := c.run(ctx, connection, managementRequestFor(connection.Binding, "inspect_identity", ""))
	if err != nil {
		return GuestIdentity{}, err
	}
	fields, err := decodeExactObject(output, "version", "machine_id", "hostname")
	if err != nil {
		return GuestIdentity{}, fmt.Errorf("parse guest identity: %w", err)
	}
	var version int
	var identity GuestIdentity
	if err := decodeField(fields, "version", &version); err != nil {
		return GuestIdentity{}, err
	}
	if err := decodeField(fields, "machine_id", &identity.MachineID); err != nil {
		return GuestIdentity{}, err
	}
	if err := decodeField(fields, "hostname", &identity.Hostname); err != nil {
		return GuestIdentity{}, err
	}
	if version != 1 || len(identity.MachineID) != 32 || identity.MachineID == strings.Repeat("0", 32) {
		return GuestIdentity{}, fmt.Errorf("guest identity version or machine ID is invalid")
	}
	for _, c := range identity.MachineID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return GuestIdentity{}, fmt.Errorf("guest machine ID is malformed")
		}
	}
	if identity.Hostname != "boxwarden-"+identity.MachineID[:12] {
		return GuestIdentity{}, fmt.Errorf("guest hostname does not match machine ID")
	}
	return identity, nil
}

func validPackageName(value string) bool {
	if len(value) == 0 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value[1:] {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '+' || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func validPackageVersion(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
}

func decodeProbeResult(contents []byte) (ProbeResult, error) {
	fields, err := decodeExactObject(contents, "version", "ok")
	if err != nil {
		return ProbeResult{}, err
	}
	var version int
	var result ProbeResult
	if err := decodeField(fields, "version", &version); err != nil {
		return ProbeResult{}, err
	}
	if err := decodeField(fields, "ok", &result.OK); err != nil {
		return ProbeResult{}, err
	}
	if version != 1 {
		return ProbeResult{}, fmt.Errorf("unsupported probe response version %d", version)
	}
	return result, nil
}

func decodeZoneResult(contents []byte) (string, error) {
	fields, err := decodeExactObject(contents, "version", "zone")
	if err != nil {
		return "", err
	}
	var version int
	var zone string
	if err := decodeField(fields, "version", &version); err != nil {
		return "", err
	}
	if err := decodeField(fields, "zone", &zone); err != nil {
		return "", err
	}
	if version != 1 {
		return "", fmt.Errorf("unsupported zone response version %d", version)
	}
	return zone, nil
}

func (c *Client) run(ctx context.Context, connection Connection, request managementRequest) ([]byte, error) {
	if c == nil || c.runner == nil {
		return nil, fmt.Errorf("management SSH runner is required")
	}
	if err := validateConnection(connection); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxManagementRequestBytes {
		return nil, fmt.Errorf("management request exceeds bound")
	}
	deadline := time.Now().Add(managementWallTimeout)
	if callerDeadline, hasDeadline := ctx.Deadline(); hasDeadline && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithDeadline(ctx, deadline)
	defer cancel()
	if err := verifyKnownHostsPin(connection); err != nil {
		return nil, fmt.Errorf("SSH known-hosts file: %w", err)
	}
	result, err := c.runner.Run(ctx, Command{Path: sshPath, Args: sshArguments(connection), Stdin: encoded})
	if err != nil {
		return nil, fmt.Errorf("strict management SSH: %w", err)
	}
	if result.Truncated || len(result.Stdout) > maxManagementResponseBytes || len(result.Stderr) > maxManagementResponseBytes {
		return nil, fmt.Errorf("management SSH output exceeds bound")
	}
	return []byte(result.Stdout), nil
}

func managementRequestFor(binding Binding, kind, zone string) managementRequest {
	return managementRequest{Version: 1, Kind: kind, Domain: string(binding.Domain), SessionID: binding.SessionID, BackendKind: binding.BackendKind, BackendObject: binding.BackendObject, Zone: zone}
}

func validateConnection(connection Connection) error {
	if _, err := netip.ParseAddr(connection.Address); err != nil {
		return fmt.Errorf("management SSH address must be a literal IP: %w", err)
	}
	if connection.Port == 0 {
		return fmt.Errorf("management SSH port is required")
	}
	if err := connection.Binding.Validate(); err != nil {
		return err
	}
	pin := connection.Pin
	if pin.Version != hostKeyPinVersion || pin.Domain != connection.Binding.Domain || pin.SessionID != connection.Binding.SessionID || pin.BackendKind != connection.Binding.BackendKind || pin.BackendObject != connection.Binding.BackendObject || pin.Algorithm != "ssh-ed25519" {
		return fmt.Errorf("host-key pin does not match SSH connection binding")
	}
	public, _, fingerprint, err := parseEd25519PublicKey(pin.PublicKey)
	if err != nil || public != pin.PublicKey || fingerprint != pin.Fingerprint {
		return fmt.Errorf("host-key pin is invalid")
	}
	for _, path := range []string{connection.RuntimeDirectory, connection.IdentityFile, connection.CertificateFile, connection.KnownHostsFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("SSH credential paths must be canonical and absolute")
		}
		// OpenSSH parses -o operands as configuration text even when each is
		// passed as one argv element. Reject characters that could escape the
		// quoted path or trigger OpenSSH token expansion.
		if strings.ContainsAny(path, "\"\\%$") || strings.IndexFunc(path, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return fmt.Errorf("SSH credential path cannot be represented safely in OpenSSH configuration")
		}
	}
	// OpenSSH pairs an IdentityFile with its conventional -cert.pub companion.
	// An explicit CertificateFile made it try that public file as a private
	// signing key on the qualified host; require and validate only this exact
	// companion path before letting OpenSSH discover it.
	if connection.CertificateFile != connection.IdentityFile+"-cert.pub" {
		return fmt.Errorf("SSH certificate must be the identity key's exact companion")
	}
	if _, err := requireRuntimeFile(connection.RuntimeDirectory, connection.IdentityFile, privateFileMode); err != nil {
		return fmt.Errorf("SSH identity file: %w", err)
	}
	if _, err := requireRuntimeFile(connection.RuntimeDirectory, connection.CertificateFile, publicFileMode); err != nil {
		return fmt.Errorf("SSH certificate file: %w", err)
	}
	if _, err := requireRuntimeFile(connection.RuntimeDirectory, connection.KnownHostsFile, privateFileMode); err != nil {
		return fmt.Errorf("SSH known-hosts file: %w", err)
	}
	return nil
}

// verifyKnownHostsPin re-reads the supervisor-owned runtime file immediately
// before ssh is invoked. File admission alone is insufficient: ssh would
// otherwise accept any well-formed key stored at the expected private path.
func verifyKnownHostsPin(connection Connection) error {
	contents, err := readRuntimeFile(connection.RuntimeDirectory, connection.KnownHostsFile, privateFileMode)
	if err != nil {
		return err
	}
	want := HostKeyAlias(connection.Binding.SessionID) + " " + connection.Pin.PublicKey + "\n"
	if string(contents) != want {
		return fmt.Errorf("does not exactly match durable host-key pin")
	}
	return nil
}

func sshArguments(connection Connection) []string {
	arguments := strictOpenSSHArguments(connection)
	arguments = append(arguments, "-p", strconv.Itoa(int(connection.Port)), "boxwarden@"+connection.Address, "/usr/bin/sudo", "-n", "--", guestManagementHelper, "management")
	return arguments
}

// strictOpenSSHArguments is shared by the fixed management helper and the
// bounded SFTP importer so neither transport can silently weaken host-key or
// credential policy.
func strictOpenSSHArguments(connection Connection) []string {
	options := []string{
		"IdentityFile=" + sshQuotedPath(connection.IdentityFile),
		"HostKeyAlias=" + HostKeyAlias(connection.Binding.SessionID), "UserKnownHostsFile=" + sshQuotedPath(connection.KnownHostsFile),
		"GlobalKnownHostsFile=/dev/null", "StrictHostKeyChecking=yes", "CheckHostIP=no", "BatchMode=yes",
		"IdentitiesOnly=yes", "IdentityAgent=none", "HostKeyAlgorithms=ssh-ed25519", "UpdateHostKeys=no",
		"PubkeyAcceptedAlgorithms=ssh-ed25519-cert-v01@openssh.com",
		"VerifyHostKeyDNS=no", "CanonicalizeHostname=no", "ProxyCommand=none", "ProxyJump=none",
		"ControlMaster=no", "ControlPath=none", "RequestTTY=no", "PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no", "ForwardAgent=no", "ForwardX11=no", "ClearAllForwardings=yes",
		"PermitLocalCommand=no", "Tunnel=no", "ConnectTimeout=10", "ServerAliveInterval=5", "ServerAliveCountMax=3",
	}
	arguments := []string{"-F", "/dev/null"}
	for _, option := range options {
		arguments = append(arguments, "-o", option)
	}
	return arguments
}

func sshQuotedPath(path string) string { return "\"" + path + "\"" }

func validZone(value string) bool {
	if len(value) == 0 || len(value) > 127 || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '/' || r == '_' || r == '-' || r == '+') {
			return false
		}
	}
	return true
}
