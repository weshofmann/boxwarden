//go:build darwin && cgo

package serialx

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/guestproto"
)

func TestSimplifiedRuntimeOwnsOnePTY(t *testing.T) {
	generation := privateGeneration(t)
	calls := 0
	r, err := createRuntime(context.Background(), generation, func() (*os.File, *os.File, error) { calls++; return allocatePTY() })
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if calls != 1 {
		t.Fatalf("PTY allocations = %d, want exactly one", calls)
	}
	if r.TartSlave() != filepath.Join(generation, "serial", "tart-serial") {
		t.Fatalf("unexpected Tart path %q", r.TartSlave())
	}
	entries, err := os.ReadDir(filepath.Dir(r.TartSlave()))
	if err != nil || len(entries) != 1 || entries[0].Name() != "tart-serial" {
		t.Fatalf("serial entries = %v, %v", entries, err)
	}
	for path, mode := range map[string]os.FileMode{filepath.Dir(r.TartSlave()): 0o700, r.TartSlave(): 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("private path %s = %v, %v", path, info, err)
		}
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(generation, "serial")); !os.IsNotExist(err) {
		t.Fatalf("serial subtree remains: %v", err)
	}
	if _, err := os.Stat(generation); err != nil {
		t.Fatalf("removed supervisor generation: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("repeat close: %v", err)
	}
}

// This exercises bootstrap and close against an actual Darwin PTY.
func TestDarwinPTYBootstrapDrainAndClose(t *testing.T) {
	r, err := CreateRuntime(context.Background(), privateGeneration(t))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	guest, err := os.OpenFile(r.TartSlave(), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer guest.Close()
	done := startBootstrap(r, context.Background())
	request := bootstrapRequest()
	// Read the two lines bytewise: terminal input must already be raw.
	var lines []byte
	for newlines := 0; newlines != 2; {
		var b [1]byte
		if _, err := guest.Read(b[:]); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, b[0])
		if b[0] == '\n' {
			newlines++
		}
	}
	if len(lines) == 0 {
		t.Fatal("no bootstrap request")
	}
	begin, end, _ := guestproto.EncodeSerialFrame(request, bootstrapResult())
	if _, err := io.WriteString(guest, begin+"\n"+end+"\n"); err != nil {
		t.Fatal(err)
	}
	awaitBootstrap(t, done, false)
	closed := make(chan error, 1)
	go func() { closed <- r.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("PTY reader did not unblock on close")
	}
}

func TestDarwinPTYTransportsRawBytesWithoutEcho(t *testing.T) {
	master, slave, err := allocatePTY()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	// Reopen nonblocking so the test has deadlines even when raw mode is broken.
	fd, err := syscall.Open(slave.Name(), syscall.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		slave.Close()
		t.Fatal(err)
	}
	slave.Close()
	guest := os.NewFile(uintptr(fd), "test-slave")
	defer guest.Close()
	guest.SetReadDeadline(time.Now().Add(time.Second))
	want := []byte{'a', '\r', 0x03, 0, 'z'}
	if _, err := master.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(guest, got); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("raw PTY bytes = %v, %v", got, err)
	}
	master.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if n, _ := master.Read(got); n != 0 {
		t.Fatalf("PTY echoed %d host-written bytes", n)
	}
}

func TestDarwinPTYDescriptorsDoNotLeakAcrossExec(t *testing.T) {
	master, slave, err := allocatePTY()
	if err != nil {
		t.Fatal(err)
	}
	defer master.Close()
	defer slave.Close()
	for _, file := range []*os.File{master, slave} {
		flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), syscall.F_GETFD, 0)
		if errno != 0 || flags&syscall.FD_CLOEXEC == 0 {
			t.Fatalf("%s would be inherited across exec: flags %d, errno %v", file.Name(), flags, errno)
		}
	}
}
