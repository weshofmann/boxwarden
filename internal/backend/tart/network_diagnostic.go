//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package tart

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"io"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

type DiagnosticMACObservation struct {
	MAC          [6]uint8
	ConfigSHA256 string
}

func (o Observer) ObserveDiagnosticMAC(ctx context.Context, object string) (result DiagnosticMACObservation, err error) {
	if ctx.Err() != nil || !o.qualified || !canonicalAbsolutePath(o.tartHome) || !canonicalAbsolutePath(o.executable) || backend.ValidateObjectID(object) != nil {
		return result, networkdiag.ErrMetadata
	}
	path := filepath.Join(o.tartHome, "vms", object, "config.json")
	var ancestry []os.FileInfo
	var paths []string
	for p := filepath.Dir(path); p != "/"; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil || !s.IsDir() {
			return result, networkdiag.ErrMetadata
		}
		ancestry = append(ancestry, s)
		paths = append(paths, p)
		if p == o.tartHome || p == filepath.Dir(path) || p == filepath.Dir(filepath.Dir(path)) {
			st, ok := s.Sys().(*syscall.Stat_t)
			if !ok || st.Uid != uint32(os.Getuid()) || s.Mode().Perm()&0022 != 0 {
				return result, networkdiag.ErrMetadata
			}
			acl, e := (hostx.OSACLInspector{}).HasExtendedACL(p)
			if e != nil || acl {
				return result, networkdiag.ErrMetadata
			}
		}
	}
	before, e := os.Lstat(path)
	if e != nil {
		return result, networkdiag.ErrMetadata
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return result, networkdiag.ErrMetadata
	}
	f := os.NewFile(uintptr(fd), "diagnostic-tart-config")
	defer func() {
		if f.Close() != nil {
			result = DiagnosticMACObservation{}
			err = networkdiag.ErrMetadata
		}
	}()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) || info.Mode().Perm() != 0600 && info.Mode().Perm() != 0644 || info.Size() < 1 || info.Size() > 65536 {
		return result, networkdiag.ErrMetadata
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 {
		return result, networkdiag.ErrMetadata
	}
	acl, e := (hostx.OSACLInspector{}).HasExtendedACL(path)
	if e != nil || acl {
		return result, networkdiag.ErrMetadata
	}
	raw, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil || len(raw) > 65536 {
		return result, networkdiag.ErrMetadata
	}
	mac, e := diagnosticConfigMAC(raw)
	if e != nil {
		return result, e
	}
	after, e := os.Lstat(path)
	current, e2 := f.Stat()
	if e != nil || e2 != nil || !os.SameFile(info, after) || !os.SameFile(info, current) || info.Size() != current.Size() || !info.ModTime().Equal(current.ModTime()) {
		return result, networkdiag.ErrMetadata
	}
	for i, p := range paths {
		now, e := os.Lstat(p)
		if e != nil || !os.SameFile(ancestry[i], now) || now.Mode() != ancestry[i].Mode() {
			return result, networkdiag.ErrMetadata
		}
	}
	if ctx.Err() != nil {
		return result, networkdiag.ErrMetadata
	}
	sum := sha256.Sum256(raw)
	return DiagnosticMACObservation{mac, hex.EncodeToString(sum[:])}, nil
}

// This catalogue follows the qualified Tart VMConfig CodingKeys. The root MAC
// is read once; config bytes are neither retained nor returned.
func diagnosticConfigMAC(raw []byte) ([6]uint8, error) {
	var zero [6]uint8
	d := json.NewDecoder(bytes.NewReader(raw))
	start, e := d.Token()
	if e != nil || start != json.Delim('{') {
		return zero, networkdiag.ErrMetadata
	}
	fields := map[string]json.RawMessage{}
	allowed := map[string]bool{}
	for _, n := range []string{"version", "os", "arch", "cpuCountMin", "cpuCount", "memorySizeMin", "memorySize", "macAddress", "display", "displayRefit", "diskFormat", "ecid", "hardwareModel"} {
		allowed[n] = true
	}
	for d.More() {
		tok, e := d.Token()
		key, ok := tok.(string)
		if e != nil || !ok || !allowed[key] || fields[key] != nil {
			return zero, networkdiag.ErrMetadata
		}
		var v json.RawMessage
		if d.Decode(&v) != nil {
			return zero, networkdiag.ErrMetadata
		}
		fields[key] = v
	}
	if end, e := d.Token(); e != nil || end != json.Delim('}') {
		return zero, networkdiag.ErrMetadata
	}
	if _, e := d.Token(); e != io.EOF {
		return zero, networkdiag.ErrMetadata
	}
	var version uint8
	var operatingSystem, arch, mac string
	if json.Unmarshal(fields["version"], &version) != nil || version != 1 || json.Unmarshal(fields["os"], &operatingSystem) != nil || operatingSystem != "linux" || json.Unmarshal(fields["arch"], &arch) != nil || arch != "arm64" || json.Unmarshal(fields["macAddress"], &mac) != nil || len(mac) != 17 {
		return zero, networkdiag.ErrMetadata
	}
	for _, n := range []string{"cpuCountMin", "cpuCount", "memorySizeMin", "memorySize"} {
		var value uint64
		if json.Unmarshal(fields[n], &value) != nil || value == 0 {
			return zero, networkdiag.ErrMetadata
		}
	}
	parsed, e := net.ParseMAC(mac)
	if e != nil || len(parsed) != 6 {
		return zero, networkdiag.ErrMetadata
	}
	copy(zero[:], parsed)
	if !networkdiag.MAC(zero) {
		return [6]uint8{}, networkdiag.ErrMetadata
	}
	return zero, nil
}
