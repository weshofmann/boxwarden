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
	maxManagementRequestBytes  = 4 << 10
	maxManagementResponseBytes = 64 << 10
	managementWallTimeout      = 30 * time.Second
)

type Connection struct {
	Address         string
	Port            uint16
	Binding         Binding
	Pin             HostKeyPin
	IdentityFile    string
	CertificateFile string
	KnownHostsFile  string
}

type Client struct{ runner Runner }

func NewClient(runner Runner) *Client { return &Client{runner: runner} }

type ProbeRequest struct{}
type ProbeResult struct {
	OK bool `json:"ok"`
}
type ApplyZoneRequest struct{ Zone string }
type ReadZoneRequest struct{}

// managementRequest is intentionally package-private: callers choose only a concrete typed method.
type managementRequest struct {
	Version       int    `json:"version"`
	Kind          string `json:"kind"`
	Domain        string `json:"domain"`
	SessionID     string `json:"session_id"`
	BackendKind   string `json:"backend_kind"`
	BackendObject string `json:"backend_object"`
	Zone          string `json:"zone,omitempty"`
}

func (c *Client) Probe(ctx context.Context, connection Connection, _ ProbeRequest) (ProbeResult, error) {
	output, err := c.run(ctx, connection, managementRequestFor(connection.Binding, "probe", ""))
	if err != nil {
		return ProbeResult{}, err
	}
	result, err := decodeProbeResult(output)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("parse probe response: %w", err)
	}
	return result, nil
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
	for _, path := range []string{connection.IdentityFile, connection.CertificateFile, connection.KnownHostsFile} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("SSH credential paths must be canonical and absolute")
		}
		if err := requirePrivateDirectory(filepath.Dir(path)); err != nil {
			return fmt.Errorf("SSH credential directory: %w", err)
		}
	}
	if _, err := requirePrivateFile(connection.IdentityFile); err != nil {
		return fmt.Errorf("SSH identity file: %w", err)
	}
	if _, err := requirePublicFile(connection.CertificateFile); err != nil {
		return fmt.Errorf("SSH certificate file: %w", err)
	}
	if _, err := requirePrivateFile(connection.KnownHostsFile); err != nil {
		return fmt.Errorf("SSH known-hosts file: %w", err)
	}
	return nil
}

func sshArguments(connection Connection) []string {
	options := []string{
		"IdentityFile=" + connection.IdentityFile, "CertificateFile=" + connection.CertificateFile,
		"HostKeyAlias=" + HostKeyAlias(connection.Binding.SessionID), "UserKnownHostsFile=" + connection.KnownHostsFile,
		"GlobalKnownHostsFile=/dev/null", "StrictHostKeyChecking=yes", "CheckHostIP=no", "BatchMode=yes",
		"IdentitiesOnly=yes", "IdentityAgent=none", "HostKeyAlgorithms=ssh-ed25519", "UpdateHostKeys=no",
		"VerifyHostKeyDNS=no", "CanonicalizeHostname=no", "ProxyCommand=none", "ProxyJump=none",
		"ControlMaster=no", "ControlPath=none", "RequestTTY=no", "PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no", "ForwardAgent=no", "ForwardX11=no", "ClearAllForwardings=yes",
		"PermitLocalCommand=no", "Tunnel=no", "ConnectTimeout=10", "ServerAliveInterval=5", "ServerAliveCountMax=3",
	}
	arguments := []string{"-F", "/dev/null"}
	for _, option := range options {
		arguments = append(arguments, "-o", option)
	}
	arguments = append(arguments, "-p", strconv.Itoa(int(connection.Port)), "boxwarden@"+connection.Address, "/usr/bin/sudo", "-n", "--", guestManagementHelper, "management")
	return arguments
}

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
