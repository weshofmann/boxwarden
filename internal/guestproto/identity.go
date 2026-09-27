package guestproto

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
)

// inspectIdentity reports only fresh clone identity facts. The guest helper
// checks fixed paths and returns no private account or machine seed material.
// A potentially malicious guest can still fabricate its own report; the host
// uses this as acceptance evidence, not a containment boundary.
func (b *Bootstrapper) inspectIdentity() ([]byte, error) {
	if b == nil || b.Root == "" || b.effectiveHostname == nil {
		return nil, errors.New("guest identity root is required")
	}
	root, err := os.OpenRoot(b.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, name := range []string{"etc/boxwarden-task0-spike", "var/lib/boxwarden/golden-clone-ready", "etc/shadow-", "var/backups/shadow.bak"} {
		if _, err := root.Lstat(name); err == nil {
			return nil, fmt.Errorf("generic build marker remains at %s", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	machine, err := readIdentityFile(root, "etc/machine-id", 64, 0444, 0644)
	if err != nil {
		return nil, fmt.Errorf("machine ID: %w", err)
	}
	machineID := strings.TrimSuffix(string(machine), "\n")
	if len(machineID) != 32 || machineID == strings.Repeat("0", 32) {
		return nil, errors.New("machine ID is not fresh")
	}
	for _, c := range machineID {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return nil, errors.New("machine ID is malformed")
		}
	}
	host, err := readIdentityFile(root, "etc/hostname", 128, 0444, 0644)
	if err != nil {
		return nil, fmt.Errorf("hostname: %w", err)
	}
	hostname := strings.TrimSuffix(string(host), "\n")
	if hostname != "boxwarden-"+machineID[:12] {
		return nil, errors.New("hostname is not derived from fresh machine ID")
	}
	effectiveHostname, err := b.effectiveHostname()
	if err != nil {
		return nil, fmt.Errorf("effective hostname: %w", err)
	}
	if effectiveHostname != hostname {
		return nil, errors.New("effective hostname does not match persisted fresh identity")
	}
	shadow, err := readIdentityFile(root, "etc/shadow", MaxRequestBytes, 0600, 0640)
	if err != nil {
		return nil, fmt.Errorf("shadow: %w", err)
	}
	accounts := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(shadow), "\n"), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 0 && fields[0] == "boxwarden" {
			accounts++
			if len(fields) != 9 || fields[1] != "!" {
				return nil, errors.New("builder password verifier is not removed")
			}
		}
	}
	if accounts != 1 {
		return nil, errors.New("expected one locked workstation account")
	}
	response, err := json.Marshal(struct {
		Version   int    `json:"version"`
		MachineID string `json:"machine_id"`
		Hostname  string `json:"hostname"`
	}{Version: Version, MachineID: machineID, Hostname: hostname})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func readIdentityFile(root *os.Root, name string, limit int64, modes ...fs.FileMode) ([]byte, error) {
	info, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return nil, errors.New("identity file has unsafe metadata")
	}
	allowed := false
	for _, mode := range modes {
		allowed = allowed || info.Mode().Perm() == mode
	}
	if !allowed || info.Size() > limit {
		return nil, errors.New("identity file has unsafe mode or length")
	}
	file, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, errors.New("identity file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("identity file exceeds bound")
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(info, after) {
		return nil, errors.New("identity file changed while reading")
	}
	return data, nil
}
