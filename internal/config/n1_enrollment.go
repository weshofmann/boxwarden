//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package config

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

const N1StateRoot = "/Volumes/BoxwardenAlphaQualification/n1-diagnostic-20260930-state"
const N1Domain = "n1qualification"
const N1ControlName = "n1diag20260930control"
const N1CandidateName = "n1diag20260930candidate"
const n1ConfigRoot = "/Users/devel/Backup/boxwarden_archive/n1-diagnostic-20260930/package/config"
const n1StockConfig = n1ConfigRoot + "/stock.enrolled.json"
const n1StockDigest = "0fe963334098b05399a6e31d4df8de5759734e5c971c4ba70e2ccbc3aac2020b"
const n1CandidateConfig = n1ConfigRoot + "/candidate.enrolled.json"
const n1CandidateDigest = "3ca4aaa4f4ebed8475d186679a08008c82db3fe737e48feda944c632d09e109c"

var ErrN1Enrollment = errors.New("exact diagnostic enrollment refused")

func LoadN1Enrollment(path string) (Config, error) {
	if path != n1SelectedConfig {
		return Config{}, ErrN1Enrollment
	}
	return loadN1Fixed(path, n1SelectedDigest)
}
func LoadN1ControlEnrollment() (Config, error) { return loadN1Fixed(n1StockConfig, n1StockDigest) }
func LoadN1CurrentEnrollment() (Config, error) {
	return loadN1Fixed(n1SelectedConfig, n1SelectedDigest)
}
func loadN1Fixed(path, digest string) (Config, error) {
	if os.Getuid() != 501 {
		return Config{}, ErrN1Enrollment
	}
	for _, p := range []string{n1ConfigRoot, filepath.Dir(n1ConfigRoot)} {
		s, err := os.Lstat(p)
		if err != nil || !s.IsDir() || s.Mode().Perm() != 0700 {
			return Config{}, ErrN1Enrollment
		}
		st, ok := s.Sys().(*syscall.Stat_t)
		if !ok || st.Uid != 501 {
			return Config{}, ErrN1Enrollment
		}
	}
	raw, err := readN1EnrollmentBytes(path, digest)
	if err != nil {
		return Config{}, ErrN1Enrollment
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	c, err := decodeConfig(d, true)
	if err != nil {
		return Config{}, ErrN1Enrollment
	}
	domains := c.Domains()
	if len(domains) != 1 || domains[0].ID != N1Domain || domains[0].StateRoot != N1StateRoot || domains[0].WorkspaceStorage == nil || domains[0].WorkspaceStorage.MountPoint != "/Volumes/BoxwardenAlphaQualification" || domains[0].WorkspaceStorage.VolumeUUID != "a178510a-d5ec-4495-828b-bd5445e2b66d" {
		return Config{}, ErrN1Enrollment
	}
	return c, nil
}
func readN1EnrollmentBytes(path, digest string) (raw []byte, err error) {
	for p := filepath.Dir(path); p != "/"; p = filepath.Dir(p) {
		s, e := os.Lstat(p)
		if e != nil || !s.IsDir() {
			return nil, ErrN1Enrollment
		}
	}
	before, e := os.Lstat(path)
	if e != nil {
		return nil, ErrN1Enrollment
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrN1Enrollment
	}
	f := os.NewFile(uintptr(fd), "diagnostic-enrollment")
	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			raw = nil
			err = ErrN1Enrollment
		}
	}()
	info, e := f.Stat()
	if e != nil || !os.SameFile(before, info) || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() <= 0 || info.Size() > 4096 {
		return nil, ErrN1Enrollment
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		return nil, ErrN1Enrollment
	}
	acl, e := (hostx.OSACLInspector{}).HasExtendedACL(path)
	if e != nil || acl {
		return nil, ErrN1Enrollment
	}
	raw, e = io.ReadAll(io.LimitReader(f, 4097))
	if e != nil || len(raw) > 4096 {
		return nil, ErrN1Enrollment
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != digest {
		return nil, ErrN1Enrollment
	}
	after, e := os.Lstat(path)
	current, e2 := f.Stat()
	if e != nil || e2 != nil || !os.SameFile(info, after) || !os.SameFile(info, current) || current.Size() != info.Size() || !current.ModTime().Equal(info.ModTime()) {
		return nil, ErrN1Enrollment
	}
	return raw, nil
}
