package guestproto

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	boxwardenRelative  = "etc/ssh/boxwarden"
	activeName         = "active"
	maxSSHDOutputBytes = 64 << 10
)

// Runner is argv-only: management never exposes an ambient shell.
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	var stdout, stderr boundedBuffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if stdout.overflow || stderr.overflow {
		return nil, fmt.Errorf("command output exceeds bound")
	}
	return stdout.Buffer.Bytes(), err
}

type boundedBuffer struct {
	bytes.Buffer
	overflow bool
}

func (b *boundedBuffer) Write(data []byte) (int, error) {
	if b.Len()+len(data) > maxSSHDOutputBytes {
		b.overflow = true
		return 0, fmt.Errorf("output exceeds bound")
	}
	return b.Buffer.Write(data)
}

type Bootstrapper struct {
	Root            string
	Runner          Runner
	HostKeyPath     string
	ZonePath        string
	Failpoint       func(string) error
	renameNoReplace func(string, string) error
}

func NewBootstrapper(root string, runner Runner) *Bootstrapper {
	if root == "" {
		root = "/"
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &Bootstrapper{Root: root, Runner: runner, HostKeyPath: "/etc/ssh/ssh_host_ed25519_key.pub", ZonePath: "/etc/timezone", renameNoReplace: renameWithoutReplacement}
}

func (b *Bootstrapper) Serial(ctx context.Context, request SerialRequest) (SerialResult, error) {
	if b == nil || b.Runner == nil || b.renameNoReplace == nil {
		return SerialResult{}, fmt.Errorf("bootstrapper is required")
	}
	if err := ctx.Err(); err != nil {
		return SerialResult{}, err
	}
	if err := request.Validate(); err != nil {
		return SerialResult{}, err
	}
	if err := b.validateBootstrapAncestors(); err != nil {
		return SerialResult{}, err
	}
	parent, err := b.path(boxwardenRelative)
	if err != nil {
		return SerialResult{}, err
	}
	if err := safeDirectory(parent, 0o755); err != nil {
		return SerialResult{}, fmt.Errorf("bootstrap parent: %w", err)
	}
	if err := rejectStaleStaging(parent); err != nil {
		return SerialResult{}, err
	}
	active := filepath.Join(parent, activeName)
	if info, err := os.Lstat(active); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return SerialResult{}, fmt.Errorf("active state is unsafe")
		}
		if err := verifyActive(active, request); err != nil {
			return SerialResult{}, err
		}
	} else if os.IsNotExist(err) {
		if err := b.publishActive(parent, active, request); err != nil {
			return SerialResult{}, err
		}
	} else {
		return SerialResult{}, err
	}
	sshd, err := b.verifySSHD(ctx)
	if err != nil {
		return SerialResult{}, err
	}
	host, err := b.readGuestFile(b.HostKeyPath, 0o644)
	if err != nil {
		return SerialResult{}, fmt.Errorf("host public key: %w", err)
	}
	if !validPublicKey(strings.TrimSpace(string(host))) {
		return SerialResult{}, fmt.Errorf("host public key is not fresh ed25519 public material")
	}
	return b.result(request, active, sshd, strings.TrimSpace(string(host)))
}
func (b *Bootstrapper) Management(ctx context.Context, request ManagementRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	if err := b.validateBootstrapAncestors(); err != nil {
		return nil, err
	}
	active, err := b.path(filepath.Join(boxwardenRelative, activeName))
	if err != nil {
		return nil, err
	}
	if err := verifyActiveAssociation(active, request.Association); err != nil {
		return nil, err
	}
	switch request.Kind {
	case "probe":
		return []byte(`{"version":1,"ok":true}`), nil
	case "read_zone":
		contents, err := b.readGuestFile(b.ZonePath, 0o644)
		if err != nil {
			return nil, err
		}
		zone := strings.TrimSpace(string(contents))
		if !validZone(zone) {
			return nil, fmt.Errorf("invalid system time zone")
		}
		return []byte(fmt.Sprintf(`{"version":1,"zone":%q}`, zone)), nil
	case "apply_zone":
		if _, err := b.Runner.Run(ctx, "/usr/bin/timedatectl", "set-timezone", request.Zone); err != nil {
			return nil, fmt.Errorf("apply time zone: %w", err)
		}
		return []byte(`{"version":1,"ok":true}`), nil
	}
	return nil, fmt.Errorf("unsupported management request")
}
func (b *Bootstrapper) path(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.HasPrefix(relative, "..") {
		return "", fmt.Errorf("unsafe bootstrap relative path")
	}
	return filepath.Join(b.Root, relative), nil
}
func (b *Bootstrapper) readGuestFile(path string, mode fs.FileMode) ([]byte, error) {
	full, err := b.path(strings.TrimPrefix(path, "/"))
	if err != nil {
		return nil, err
	}
	return readSafeFile(full, mode)
}
func (b *Bootstrapper) validateBootstrapAncestors() error {
	root, err := os.OpenRoot(b.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, path := range []string{"etc", "etc/ssh", boxwardenRelative} {
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o755 || !ownedByCurrentUser(info) {
			return fmt.Errorf("unsafe bootstrap path component %q", path)
		}
	}
	return nil
}

func (b *Bootstrapper) publishActive(parent, active string, request SerialRequest) error {
	stage, err := os.MkdirTemp(parent, ".boxwarden-staging-")
	if err != nil {
		return err
	}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := os.Chmod(stage, 0o700); err != nil {
		return err
	}
	principals := filepath.Join(stage, "authorized_principals")
	if err := os.Mkdir(principals, 0o700); err != nil {
		return err
	}
	manifest, err := json.Marshal(bindingManifest{Version: Version, Association: request.Association, CAFingerprint: request.CAFingerprint, Principal: request.Principal})
	if err != nil {
		return err
	}
	if err := b.fail("before-ca"); err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(stage, "trusted-user-ca.pub"), []byte(strings.TrimSpace(request.CAPublicKey)+"\n"), 0o644); err != nil {
		return err
	}
	if err := b.fail("before-principal"); err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(principals, "boxwarden"), []byte(request.Principal+"\n"), 0o644); err != nil {
		return err
	}
	if err := b.fail("before-manifest"); err != nil {
		return err
	}
	if err := writeSynced(filepath.Join(stage, "management-binding.json"), append(manifest, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(principals, 0o755); err != nil {
		return err
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return err
	}
	if err := verifyActive(stage, request); err != nil {
		return err
	}
	if err := syncDir(principals); err != nil {
		return err
	}
	if err := syncDir(stage); err != nil {
		return err
	}
	if _, err := os.Lstat(active); !os.IsNotExist(err) {
		if err == nil {
			return fmt.Errorf("active state appeared during publication")
		}
		return err
	}
	if err := b.fail("before-publish"); err != nil {
		return err
	}
	if err := b.renameNoReplace(stage, active); err != nil {
		return err
	}
	if err := syncDir(parent); err != nil {
		return err
	}
	failed = false
	return nil
}

type bindingManifest struct {
	Version int `json:"version"`
	Association
	CAFingerprint string `json:"ca_fingerprint"`
	Principal     string `json:"principal"`
}

func verifyActive(active string, request SerialRequest) error {
	if err := verifyActiveAssociation(active, request.Association); err != nil {
		return err
	}
	ca, err := readSafeFile(filepath.Join(active, "trusted-user-ca.pub"), 0o644)
	if err != nil {
		return err
	}
	principal, err := readSafeFile(filepath.Join(active, "authorized_principals", "boxwarden"), 0o644)
	if err != nil {
		return err
	}
	manifest, err := readManifest(filepath.Join(active, "management-binding.json"))
	if err != nil {
		return err
	}
	if manifest.Association != request.Association || manifest.CAFingerprint != request.CAFingerprint || manifest.Principal != request.Principal || string(ca) != strings.TrimSpace(request.CAPublicKey)+"\n" || string(principal) != request.Principal+"\n" {
		return fmt.Errorf("existing bootstrap state differs")
	}
	return safeTree(active)
}
func verifyActiveAssociation(active string, association Association) error {
	manifest, err := readManifest(filepath.Join(active, "management-binding.json"))
	if err != nil {
		return fmt.Errorf("management binding manifest: %w", err)
	}
	if manifest.Version != Version || manifest.Association != association || manifest.Principal != derivedPrincipal(association.SessionID) {
		return fmt.Errorf("management association differs")
	}
	return safeTree(active)
}
func readManifest(path string) (bindingManifest, error) {
	contents, err := readSafeFile(path, 0o600)
	if err != nil {
		return bindingManifest{}, err
	}
	fields, err := exactObject(contents, "version", "domain", "session_id", "backend_kind", "backend_object", "ca_fingerprint", "principal")
	if err != nil {
		return bindingManifest{}, fmt.Errorf("invalid binding manifest")
	}
	var value bindingManifest
	if err := decodeFields(fields, &value); err != nil || !value.Association.valid() || !validFingerprint(value.CAFingerprint) || value.Principal != derivedPrincipal(value.SessionID) {
		return bindingManifest{}, fmt.Errorf("invalid binding manifest")
	}
	return value, nil
}
func (b *Bootstrapper) fail(point string) error {
	if b.Failpoint != nil {
		return b.Failpoint(point)
	}
	return nil
}

// safeTree permits precisely the durable files the helper creates. An extra
// entry is a failure, rather than state the privileged management path ignores.
func safeTree(active string) error {
	if err := safeDirectory(active, 0o755); err != nil {
		return err
	}
	if err := exactEntries(active, map[string]bool{"trusted-user-ca.pub": true, "authorized_principals": true, "management-binding.json": true}); err != nil {
		return err
	}
	principals := filepath.Join(active, "authorized_principals")
	if err := safeDirectory(principals, 0o755); err != nil {
		return err
	}
	return exactEntries(principals, map[string]bool{"boxwarden": true})
}
func exactEntries(path string, expected map[string]bool) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("unexpected bootstrap entries")
	}
	for _, entry := range entries {
		if !expected[entry.Name()] {
			return fmt.Errorf("unexpected bootstrap entry %q", entry.Name())
		}
	}
	return nil
}
func rejectStaleStaging(parent string) error {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".boxwarden-staging-") {
			return fmt.Errorf("stale bootstrap staging directory")
		}
	}
	return nil
}
func safeDirectory(path string, mode fs.FileMode) error {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer parent.Close()
	info, err := parent.Lstat(filepath.Base(path))
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || !ownedByCurrentUser(info) {
		return fmt.Errorf("unsafe directory")
	}
	return nil
}
func readSafeFile(path string, mode fs.FileMode) ([]byte, error) {
	parent, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	name := filepath.Base(path)
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != mode || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("unsafe file")
	}
	file, err := parent.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("file changed while opening")
	}
	contents, err := io.ReadAll(io.LimitReader(file, MaxRequestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > MaxRequestBytes {
		return nil, fmt.Errorf("file exceeds bound")
	}
	after, err := parent.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return nil, fmt.Errorf("file changed while reading")
	}
	return contents, nil
}
func ownedByCurrentUser(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Geteuid() && int(stat.Gid) == os.Getegid()
}
func writeSynced(path string, data []byte, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if err = writeExact(file, data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeExact(writer io.Writer, data []byte) error {
	written, err := writer.Write(data)
	if err != nil {
		return err
	}
	if written != len(data) {
		return io.ErrShortWrite
	}
	return nil
}
func syncDir(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
func (b *Bootstrapper) verifySSHD(ctx context.Context) (map[string]string, error) {
	if _, err := b.Runner.Run(ctx, "/usr/sbin/sshd", "-t"); err != nil {
		return nil, fmt.Errorf("validate sshd: %w", err)
	}
	output, err := b.Runner.Run(ctx, "/usr/sbin/sshd", "-T", "-C", "user=boxwarden,host=localhost,addr=127.0.0.1")
	if err != nil {
		return nil, fmt.Errorf("inspect sshd: %w", err)
	}
	if len(output) > maxSSHDOutputBytes {
		return nil, fmt.Errorf("effective sshd output exceeds bound")
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			fields[parts[0]] = parts[1]
		}
	}
	for key, want := range requiredSSHD {
		if fields[key] != want {
			return nil, fmt.Errorf("effective sshd %s = %q", key, fields[key])
		}
	}
	guards := make(map[string]string, len(requiredSSHD))
	for key, value := range requiredSSHD {
		guards[key] = value
	}
	return guards, nil
}
func (b *Bootstrapper) result(request SerialRequest, active string, sshd map[string]string, host string) (SerialResult, error) {
	digests := map[string]string{}
	for _, name := range []string{"trusted-user-ca.pub", "authorized_principals/boxwarden", "management-binding.json"} {
		mode := fs.FileMode(0o644)
		if name == "management-binding.json" {
			mode = 0o600
		}
		contents, err := readSafeFile(filepath.Join(active, name), mode)
		if err != nil {
			return SerialResult{}, err
		}
		sum := sha256.Sum256(contents)
		digests[name] = hex.EncodeToString(sum[:])
	}
	return SerialResult{Version: Version, StartGeneration: request.StartGeneration, Association: request.Association, CAFingerprint: request.CAFingerprint, Principal: request.Principal, InstalledSHA256: digests, SSHD: sshd, HostPublicKey: host}, nil
}
