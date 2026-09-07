package supervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const (
	requestName        = "supervisor-request.json"
	socketName         = "supervisor.sock"
	lockName           = "generation.lock"
	maxDiagnosticBytes = 2048
	maxControlBytes    = 16 << 10
)

type Binding struct{ Domain, SessionID, BackendKind, BackendObject, Generation string }

// LaunchRequest is an expected binding, not authority. Later composition must
// reload the configured domain and durable session record before starting Tart.
type LaunchRequest struct {
	Binding           Binding `json:"binding"`
	RuntimeDirectory  string  `json:"runtime_directory"`
	HostConfigPath    string  `json:"host_config_path"`
	SessionRecordName string  `json:"session_record_name"`
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
type Launcher interface {
	Launch(context.Context, LaunchRequest) error
}

func (b Binding) valid() bool {
	return validPart(b.Domain) && validPart(b.SessionID) && validPart(b.BackendKind) && validPart(b.BackendObject) && validPart(b.Generation)
}
func validPart(s string) bool {
	if len(s) == 0 || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
func canonicalAbsolute(path string) bool {
	return len(path) <= 4096 && filepath.IsAbs(path) && filepath.Clean(path) == path && path != "/"
}
func validLaunchRequest(r LaunchRequest) error {
	if !r.Binding.valid() || !canonicalAbsolute(r.RuntimeDirectory) || !canonicalAbsolute(r.HostConfigPath) || !validPart(r.SessionRecordName) {
		return fmt.Errorf("invalid supervisor launch binding or locator")
	}
	if filepath.Base(r.RuntimeDirectory) != r.Binding.Generation || filepath.Base(filepath.Dir(r.RuntimeDirectory)) != r.Binding.SessionID || filepath.Base(filepath.Dir(filepath.Dir(r.RuntimeDirectory))) != r.Binding.Domain {
		return fmt.Errorf("runtime directory does not match exact binding")
	}
	data, err := json.Marshal(r)
	if err != nil || len(data) > maxControlBytes {
		return fmt.Errorf("encoded launch request exceeds bound")
	}
	return nil
}
func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Getuid())
}
func privateDirectory(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.IsDir() && info.Mode().Perm() == 0700 && ownedByCurrentUser(info)
}

// Reject symlink components even above the private generation hierarchy.
func safeParents(path string) error {
	for p := path; p != "/"; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("unsafe directory component %q", p)
		}
	}
	return nil
}
func openPrivateFile(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || !ownedByCurrentUser(info) {
		return nil, fmt.Errorf("not an owner-private regular file: %s", path)
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), path), nil
}
func readLaunchRequest(path string) (LaunchRequest, error) {
	var r LaunchRequest
	if filepath.Base(path) != requestName || !privateDirectory(filepath.Dir(path)) {
		return r, fmt.Errorf("unsafe launch request path")
	}
	if err := safeParents(filepath.Dir(path)); err != nil {
		return r, err
	}
	file, err := openPrivateFile(path)
	if err != nil {
		return r, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxControlBytes+1))
	if err != nil {
		return r, err
	}
	if err = decodeExact(data, &r); err != nil {
		return r, err
	}
	if err = validLaunchRequest(r); err != nil {
		return r, err
	}
	if r.RuntimeDirectory != filepath.Dir(path) {
		return r, fmt.Errorf("request runtime directory mismatch")
	}
	return r, nil
}

// Both endpoints are cooperating host code. Canonical typed JSON rejects
// unknown/duplicate fields, alternate casing, trailing values and excess bytes.
func decodeExact(data []byte, value any) error {
	if len(data) == 0 || len(data) > maxControlBytes {
		return fmt.Errorf("message exceeds bound")
	}
	if err := json.Unmarshal(data, value); err != nil {
		return err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if !bytes.Equal(data, canonical) {
		return fmt.Errorf("noncanonical typed message")
	}
	return nil
}
func snapshotReady(s Snapshot) bool {
	return s.BackendRunning && s.BrokerHealthy && s.ScreenHealthy && s.PinPresent && s.CertificateCurrent && s.ProbeOK && s.ZoneMatches
}
