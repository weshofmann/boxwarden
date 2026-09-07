package supervisor

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestControlTransportSupportsLongCanonicalGenerationPaths(t *testing.T) {
	for _, long := range []bool{false, true} {
		name := "short"
		if long {
			name = "long"
		}
		t.Run(name, func(t *testing.T) {
			request := minimalRequest(t)
			if long {
				request.RuntimeDirectory = filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(request.RuntimeDirectory))), strings.Repeat("x", 100), request.Binding.Domain, request.Binding.SessionID, request.Binding.Generation)
			}
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			held, err := acquireGenerationLock(request)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			realPath := filepath.Join(request.RuntimeDirectory, socketName)
			listener, err := listenSocket(realPath)
			if err != nil {
				t.Fatalf("listen exact generation (%d bytes): %v", len(realPath), err)
			}
			defer listener.Close()
			connection, err := dialControl(context.Background(), realPath)
			if err != nil {
				t.Fatal(err)
			}
			if long {
				alias := connection.RemoteAddr().String()
				if len(alias) >= 104 || alias == realPath {
					t.Fatalf("connect did not use short transport: %q", alias)
				}
				if _, err := os.Lstat(filepath.Dir(filepath.Dir(alias))); !os.IsNotExist(err) {
					t.Fatalf("connect alias retained: %v", err)
				}
			}
			connection.Close()
			info, err := os.Lstat(realPath)
			if err != nil || info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0600 {
				t.Fatalf("real socket placement = %v %v", info, err)
			}
			if long {
				alias := listener.Addr().String()
				if len(alias) >= 104 || alias == realPath {
					t.Fatalf("not a short transport address: %q", alias)
				}
				if _, err := os.Lstat(filepath.Dir(filepath.Dir(alias))); !os.IsNotExist(err) {
					t.Fatalf("bind alias retained: %v", err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			owner := &runtimeFixture{binding: request.Binding, done: make(chan struct{})}
			served := make(chan error, 1)
			go func() { served <- serveControl(ctx, listener, request.Binding, owner, func() error { return nil }) }()
			client := &Client{RuntimeDirectory: request.RuntimeDirectory, MaxSnapshotAge: time.Minute}
			got, err := client.Snapshot(context.Background(), request.Binding)
			if err != nil || got.Binding != request.Binding || !got.BackendRunning || !got.SerialHealthy {
				t.Fatalf("snapshot over exact socket = %#v %v", got, err)
			}
			cancel()
			if err := listener.Close(); err != nil {
				t.Fatal(err)
			}
			if err := <-served; err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(realPath); !os.IsNotExist(err) {
				t.Fatalf("close retained real socket: %v", err)
			}
		})
	}
}

func TestSocketAddressIsPrivateTransientAndRejectsRuntimeSymlinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, strings.Repeat("g", 110))
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	address, cleanup, err := socketAddress(filepath.Join(directory, socketName))
	if err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Dir(filepath.Dir(address))
	if !privateDirectory(aliasRoot) {
		t.Fatal("transport alias is not owner-private")
	}
	if got, err := os.Readlink(filepath.Dir(address)); err != nil || got != directory {
		t.Fatalf("alias target = %q %v", got, err)
	}
	if err := cleanup(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(aliasRoot); !os.IsNotExist(err) {
		t.Fatalf("alias retained: %v", err)
	}
	alias := filepath.Join(root, "runtime-alias")
	if err := os.Symlink(directory, alias); err != nil {
		t.Fatal(err)
	}
	if _, _, err := socketAddress(filepath.Join(alias, socketName)); err == nil {
		t.Fatal("adopted symlink as runtime authority")
	}
	if _, err := dialControl(context.Background(), filepath.Join(directory, socketName)); err == nil {
		t.Fatal("dial of missing socket succeeded")
	}
	shortRoot, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := filepath.Glob(filepath.Join(shortRoot, "bw-sock-*", "g"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if target, _ := os.Readlink(entry); target == directory {
			t.Fatalf("failed dial retained alias %s", entry)
		}
	}
}

func TestControlListenerCloseRefusesReplacementSocketEntry(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.RuntimeDirectory, socketName)
	listener, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	if err := listener.Close(); err == nil {
		t.Fatal("close accepted a replacement socket")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replacement socket removed: %v", err)
	}
}

func TestControlListenerCloseRejectsSymlinkedGeneration(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.RuntimeDirectory, socketName)
	listener, err := listenSocket(path)
	if err != nil {
		t.Fatal(err)
	}
	moved := request.RuntimeDirectory + "-moved"
	if err := os.Rename(request.RuntimeDirectory, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, request.RuntimeDirectory); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err == nil {
		t.Fatal("close accepted symlinked generation")
	}
	if _, err := os.Lstat(filepath.Join(moved, socketName)); err != nil {
		t.Fatalf("socket removed through foreign runtime symlink: %v", err)
	}
}
