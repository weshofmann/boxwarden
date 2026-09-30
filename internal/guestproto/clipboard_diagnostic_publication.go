//go:build n1clipboarddiagnostic

package guestproto

import (
	"os"
	"path/filepath"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
)

type diagnosticDirectory interface {
	Sync() error
	Close() error
}

func syncDiagnosticDirectory(directory string) error {
	return syncDiagnosticDirectoryWith(directory, func(path string) (diagnosticDirectory, error) { return os.Open(path) })
}
func syncDiagnosticDirectoryWith(directory string, open func(string) (diagnosticDirectory, error)) error {
	file, err := open(directory)
	if err != nil {
		return clipboarddiag.ErrMetadata
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil || closeErr != nil {
		return clipboarddiag.ErrMetadata
	}
	return nil
}

// A final name is provisional while its exact pending hardlink remains. Every
// fallible data/seal/directory operation finishes before the single unlink commit.
// No post-commit sync/close or absence-based recovery can manufacture success.
type diagnosticPublicationFile interface {
	diagnosticPendingFile
	Stat() (os.FileInfo, error)
}

func finalizeDiagnosticPending(directory, name string, file diagnosticPublicationFile, raw []byte, seal func(string, string) error) error {
	return finalizeDiagnosticPendingWith(directory, name, file, raw, seal, syncDiagnosticDirectory, syscall.Unlink)
}
func finalizeDiagnosticPendingWith(directory, name string, file diagnosticPublicationFile, raw []byte, seal func(string, string) error, finalize func(string) error, unlink func(string) error) error {
	pending := filepath.Join(directory, name+".pending")
	final := filepath.Join(directory, name)
	opened, err := file.Stat()
	if err != nil {
		file.Close()
		return clipboarddiag.ErrMetadata
	}
	return finishDiagnosticPending(file, raw, func() error {
		if seal == nil || seal(pending, final) != nil {
			return clipboarddiag.ErrMetadata
		}
		if !diagnosticPendingPair(pending, final, opened, len(raw)) {
			return clipboarddiag.ErrMetadata
		}
		if finalize(directory) != nil {
			return clipboarddiag.ErrMetadata
		}
		if !diagnosticPendingPair(pending, final, opened, len(raw)) {
			return clipboarddiag.ErrMetadata
		}
		// Successful return is required before emitting the host-bound witness.
		// Namespace absence alone never proves this syscall returned nil. An error is never
		// inferred successful from path absence, retried or used to remove other state.
		if unlink(pending) != nil {
			return clipboarddiag.ErrMetadata
		}
		return nil
	})
}
func diagnosticPendingPair(pending, final string, opened os.FileInfo, size int) bool {
	p, e := os.Lstat(pending)
	f, fe := os.Lstat(final)
	if e != nil || fe != nil || !os.SameFile(opened, p) || !os.SameFile(p, f) || !p.Mode().IsRegular() || !f.Mode().IsRegular() || p.Mode().Perm() != 0600 || f.Mode().Perm() != 0600 || p.Size() != int64(size) || f.Size() != p.Size() || !f.ModTime().Equal(p.ModTime()) {
		return false
	}
	ps, pok := p.Sys().(*syscall.Stat_t)
	fs, fok := f.Sys().(*syscall.Stat_t)
	original, ook := opened.Sys().(*syscall.Stat_t)
	return pok && fok && ook && ps.Nlink == 2 && fs.Nlink == 2 && ps.Uid == original.Uid && ps.Gid == original.Gid && fs.Uid == ps.Uid && fs.Gid == ps.Gid
}
func diagnosticPublicationCommitted(final string) bool {
	if _, err := os.Lstat(final + ".pending"); !os.IsNotExist(err) {
		return false
	}
	info, err := os.Lstat(final)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Nlink == 1
}
