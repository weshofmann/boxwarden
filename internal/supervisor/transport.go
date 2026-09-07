package supervisor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// Darwin's sockaddr_un holds 104 bytes including the terminating NUL. Keep
// runtime authority in the canonical generation; an ephemeral alias is only
// an address for the bind/connect system call, never an admitted state path.
func socketAddress(path string) (string, func() error, error) {
	if filepath.Base(path) != socketName || !canonicalAbsolute(path) || !privateDirectory(filepath.Dir(path)) {
		return "", nil, fmt.Errorf("unsafe control socket path")
	}
	if err := safeParents(filepath.Dir(path)); err != nil {
		return "", nil, err
	}
	if len(path) < 104 {
		return path, func() error { return nil }, nil
	}
	root, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return "", nil, err
	}
	if err := safeParents(root); err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp(root, "bw-sock-")
	if err != nil {
		return "", nil, err
	}
	link := filepath.Join(directory, "g")
	address := filepath.Join(link, socketName)
	if len(address) >= 104 {
		return "", nil, errors.Join(fmt.Errorf("temporary control address is too long"), os.Remove(directory))
	}
	if err := os.Symlink(filepath.Dir(path), link); err != nil {
		return "", nil, errors.Join(err, os.Remove(directory))
	}
	cleanup := func() error {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		target, err := os.Readlink(link)
		if err != nil || target != filepath.Dir(path) || !privateDirectory(directory) || len(entries) != 1 || entries[0].Name() != "g" {
			return fmt.Errorf("temporary control address changed; refuse cleanup")
		}
		if err := os.Remove(link); err != nil {
			return err
		}
		return os.Remove(directory)
	}
	return address, cleanup, nil
}

// The temporary bind address is gone by the time listenSocket returns, so
// automatic unlink cannot remove the real socket. Retain its inode identity
// and unlink only that exact socket after closing the listener.
type controlListener struct {
	*net.UnixListener
	path string
	info os.FileInfo
	once sync.Once
	err  error
}

func (l *controlListener) Close() error {
	l.once.Do(func() {
		l.err = l.UnixListener.Close()
		if err := safeParents(filepath.Dir(l.path)); err != nil {
			l.err = errors.Join(l.err, err)
			return
		}
		if !privateDirectory(filepath.Dir(l.path)) {
			l.err = errors.Join(l.err, fmt.Errorf("control generation changed; refuse unlink"))
			return
		}
		info, err := os.Lstat(l.path)
		if err != nil {
			l.err = errors.Join(l.err, err)
			return
		}
		if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 || !os.SameFile(info, l.info) || !ownedByCurrentUser(info) {
			l.err = errors.Join(l.err, fmt.Errorf("control socket changed; refuse unlink"))
			return
		}
		l.err = errors.Join(l.err, os.Remove(l.path))
	})
	return l.err
}

func dialControl(ctx context.Context, path string) (net.Conn, error) {
	address, cleanup, err := socketAddress(path)
	if err != nil {
		return nil, err
	}
	connection, err := (&net.Dialer{}).DialContext(ctx, "unix", address)
	err = errors.Join(err, cleanup())
	if err != nil {
		if connection != nil {
			_ = connection.Close()
		}
		return nil, err
	}
	return connection, nil
}
