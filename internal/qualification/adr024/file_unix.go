//go:build darwin || linux

package adr024

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

type FileEvidence struct {
	Path              string `json:"path"`
	SHA256            string `json:"sha256"`
	Device            uint64 `json:"device"`
	Inode             uint64 `json:"inode"`
	UID               int    `json:"uid"`
	GID               int    `json:"gid"`
	Mode              uint32 `json:"mode"`
	Links             uint64 `json:"links"`
	Size              int64  `json:"size"`
	ModifiedUnixNanos int64  `json:"modified_unix_nanos"`
}

type filePolicy struct {
	UID, GID int
	Mode     uint32
	Links    uint64
	SHA256   string
	MaxBytes int64
}

func readAdmittedFile(path string, policy filePolicy) (FileEvidence, []byte, error) {
	if policy.SHA256 == "" {
		return FileEvidence{}, nil, errors.New("fixed expected file digest is required")
	}
	file, evidence, err := openAdmittedFile(path, policy)
	if err != nil {
		return FileEvidence{}, nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, policy.MaxBytes+1))
	if err != nil || int64(len(raw)) > policy.MaxBytes {
		return FileEvidence{}, nil, errors.New("bounded file read failed")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(raw))
	if digest != policy.SHA256 {
		return FileEvidence{}, nil, errors.New("file digest does not match fixed expected identity")
	}
	evidence.SHA256 = digest
	if err := revalidatePath(path, evidence); err != nil {
		return FileEvidence{}, nil, err
	}
	return evidence, raw, nil
}

func readFixedManifest(path string, policy filePolicy) (FileEvidence, []byte, error) {
	if policy.SHA256 != "" {
		return FileEvidence{}, nil, errors.New("manifest digest is observed from the admitted fixed file, not caller-supplied")
	}
	file, evidence, err := openAdmittedFile(path, policy)
	if err != nil {
		return FileEvidence{}, nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, policy.MaxBytes+1))
	if err != nil || int64(len(raw)) > policy.MaxBytes {
		return FileEvidence{}, nil, errors.New("bounded fixed manifest read failed")
	}
	evidence.SHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	if err := revalidatePath(path, evidence); err != nil {
		return FileEvidence{}, nil, err
	}
	return evidence, raw, nil
}

func openAdmittedFile(path string, policy filePolicy) (*os.File, FileEvidence, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || policy.MaxBytes <= 0 || policy.Links == 0 {
		return nil, FileEvidence{}, errors.New("invalid fixed file admission input")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, FileEvidence{}, errors.New("file path must not resolve through a symlink")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return nil, FileEvidence{}, errors.New("file path is not a direct regular file")
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, FileEvidence{}, err
	}
	file := os.NewFile(uintptr(fd), path)
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, FileEvidence{}, err
	}
	evidence, err := fileEvidence(path, opened)
	if err != nil {
		file.Close()
		return nil, FileEvidence{}, err
	}
	beforeEvidence, beforeErr := fileEvidence(path, before)
	if beforeErr != nil || !sameFileEvidence(beforeEvidence, evidence) {
		file.Close()
		return nil, FileEvidence{}, errors.New("file identity changed during nofollow open")
	}
	if policy.UID >= 0 && evidence.UID != policy.UID || policy.GID >= 0 && evidence.GID != policy.GID || policy.Mode != 0 && evidence.Mode != policy.Mode || evidence.Links != policy.Links {
		file.Close()
		return nil, FileEvidence{}, errors.New("file metadata does not match fixed expected identity")
	}
	return file, evidence, nil
}

func inspectAdmittedArtifact(path string, policy filePolicy) (FileEvidence, error) {
	file, evidence, err := openAdmittedFile(path, policy)
	if err != nil {
		return FileEvidence{}, err
	}
	defer file.Close()
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, policy.MaxBytes+1))
	if err != nil || written > policy.MaxBytes {
		return FileEvidence{}, errors.New("bounded artifact read failed")
	}
	evidence.SHA256 = fmt.Sprintf("%x", hash.Sum(nil))
	if evidence.SHA256 != policy.SHA256 {
		return FileEvidence{}, errors.New("artifact digest does not match fixed expected identity")
	}
	if err := revalidatePath(path, evidence); err != nil {
		return FileEvidence{}, err
	}
	return evidence, nil
}

func revalidatePath(path string, expected FileEvidence) error {
	after, err := os.Lstat(path)
	if err != nil {
		return err
	}
	evidence, err := fileEvidence(path, after)
	if err != nil || !sameFileEvidence(expected, evidence) {
		return errors.New("file identity changed during admission")
	}
	return nil
}

func fileEvidence(path string, info os.FileInfo) (FileEvidence, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return FileEvidence{}, errors.New("unsupported filesystem metadata")
	}
	return FileEvidence{
		Path: path, Device: uint64(stat.Dev), Inode: uint64(stat.Ino), UID: int(stat.Uid), GID: int(stat.Gid),
		Mode: uint32(stat.Mode) & 0o7777, Links: uint64(stat.Nlink), Size: info.Size(), ModifiedUnixNanos: info.ModTime().UnixNano(),
	}, nil
}

func sameFileEvidence(left, right FileEvidence) bool {
	left.SHA256 = ""
	right.SHA256 = ""
	return left == right
}
