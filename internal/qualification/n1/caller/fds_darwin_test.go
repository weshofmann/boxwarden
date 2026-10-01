//go:build darwin && cgo

package caller

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
)

func TestCallerInheritedDescriptorAboveLoweredCur(t *testing.T) {
	const inherited = 192
	const success = "contained above-Cur descriptor; standard and fd3 intact\n"
	mode := os.Getenv("N1_DESCRIPTOR_FIXTURE")
	if mode == "reader" {
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, inherited, syscall.F_GETFD, 0); e != syscall.EBADF {
			os.Exit(13)
		}
		for fd := uintptr(0); fd <= 3; fd++ {
			flags, _, e := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_GETFD, 0)
			if e != 0 || flags&syscall.FD_CLOEXEC != 0 {
				os.Exit(14)
			}
		}
		input, e := io.ReadAll(os.Stdin)
		if e != nil || string(input) != "standard input\n" {
			os.Exit(15)
		}
		pipe, e := os.ReadFile("/dev/fd/3")
		if e != nil || string(pipe) != "transition\n" {
			os.Exit(16)
		}
		if n, e := os.Stdout.WriteString(success); e != nil || n != len(success) {
			os.Exit(17)
		}
		if n, e := os.Stderr.Write(nil); e != nil || n != 0 {
			os.Exit(18)
		}
		os.Exit(0)
	}
	if mode == "parent" {
		original, e := os.Open("/dev/null")
		if e != nil {
			os.Exit(20)
		}
		if syscall.Dup2(int(original.Fd()), inherited) != nil {
			os.Exit(21)
		}
		if original.Close() != nil {
			os.Exit(22)
		}
		flags, _, ce := syscall.Syscall(syscall.SYS_FCNTL, inherited, syscall.F_GETFD, 0)
		if ce != 0 || flags&syscall.FD_CLOEXEC != 0 {
			os.Exit(23)
		}
		var limit syscall.Rlimit
		if syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit) != nil || limit.Cur <= 64 {
			os.Exit(24)
		}
		limit.Cur = 64
		if syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit) != nil {
			os.Exit(25)
		}
		if _, _, ce = syscall.Syscall(syscall.SYS_FCNTL, inherited, syscall.F_GETFD, 0); ce != 0 {
			os.Exit(26)
		}
		// Actual Darwin directory observation proves this existing high descriptor
		// remains listed after lowering Cur; no PID or process list is consulted.
		directory, e := os.Open("/dev/fd")
		if e != nil {
			os.Exit(27)
		}
		entries, re := directory.ReadDir(65537)
		if re == nil && len(entries) <= 65536 {
			tail, te := directory.ReadDir(1)
			if len(tail) != 0 {
				os.Exit(28)
			}
			re = te
		}
		de := directory.Close()
		if re != io.EOF || de != nil || len(entries) > 65536 {
			os.Exit(28)
		}
		found := false
		for _, entry := range entries {
			if entry.Name() == fmt.Sprint(inherited) {
				found = true
			}
		}
		if !found {
			os.Exit(29)
		}
		r, w, e := os.Pipe()
		if e != nil {
			os.Exit(30)
		}
		n, we := w.WriteString("transition\n")
		closeErr := w.Close()
		if we != nil || closeErr != nil || n != 11 {
			os.Exit(31)
		}
		if replaceImage(os.Args[0], []string{os.Args[0], "-test.run=^TestCallerInheritedDescriptorAboveLoweredCur$"}, []string{"N1_DESCRIPTOR_FIXTURE=reader"}, r) != nil {
			os.Exit(32)
		}
		os.Exit(33)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCallerInheritedDescriptorAboveLoweredCur$")
	cmd.Env = append(os.Environ(), "N1_DESCRIPTOR_FIXTURE=parent", "GORACE=atexit_sleep_ms=0")
	cmd.Stdin = strings.NewReader("standard input\n")
	raw, e := cmd.CombinedOutput()
	if e != nil || string(raw) != success {
		t.Fatal("actual high descriptor containment", e, string(raw))
	}
}

func TestCallerImageReplacementFixture(t *testing.T) {
	mode := os.Getenv("N1_CALLER_FIXTURE")
	if mode == "reader" {
		raw, e := os.ReadFile("/dev/fd/3")
		if e != nil || string(raw) != "transition\n" {
			os.Exit(8)
		}
		os.Stdout.WriteString("replacement")
		os.Exit(0)
	}
	if mode == "parent" {
		r, w, e := os.Pipe()
		if e != nil {
			os.Exit(9)
		}
		w.WriteString("transition\n")
		w.Close()
		if replaceImage(os.Args[0], []string{os.Args[0], "-test.run=TestCallerImageReplacementFixture"}, []string{"N1_CALLER_FIXTURE=reader"}, r) != nil {
			os.Exit(10)
		}
		os.Exit(11)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCallerImageReplacementFixture")
	cmd.Env = []string{"N1_CALLER_FIXTURE=parent", "GORACE=atexit_sleep_ms=0"}
	out, e := cmd.Output()
	if e != nil || string(out) != "replacement" {
		t.Fatalf("actual exec: %s %v", out, e)
	}
}

func TestCallerNativeDescriptorRefusalControls(t *testing.T) {
	if _, _, e := nativeDescriptorNames(-1); e == nil {
		t.Fatal("actual invalid-base duplicate accepted")
	}
	ordinary, e := os.Open("/dev/null")
	if e != nil {
		t.Fatal(e)
	}
	_, _, refused := nativeDescriptorNames(int(ordinary.Fd()))
	_, stillOwned := ordinary.Stat()
	closed := ordinary.Close()
	if refused == nil || stillOwned != nil || closed != nil {
		t.Fatal("actual non-directory fdopendir failure/owned-base close", refused, stillOwned, closed)
	}
	for _, x := range []struct {
		name string
		want int
		good bool
	}{
		{"0", 0, true}, {"192", 192, true}, {"2147483647", 2147483647, true},
		{"", 0, false}, {"00", 0, false}, {"01", 0, false}, {"-1", 0, false},
		{"2147483648", 0, false}, {"10000000000", 0, false}, {"19x", 0, false}, {"19\x00", 0, false},
	} {
		got, e := nativeDescriptorNumber([]byte(x.name))
		if (e == nil) != x.good || x.good && got != x.want {
			t.Fatal("native name admission", x.name, got, e)
		}
	}
	good := descriptorDirectoryProof{duplicate: 5, count: 6, eof: true, cloexec: true, opened: true, closed: true}
	if !good.valid(4) {
		t.Fatal("complete explicit-close proof refused")
	}
	for _, mutate := range []func(*descriptorDirectoryProof){
		func(p *descriptorDirectoryProof) { p.duplicate = 4 }, func(p *descriptorDirectoryProof) { p.duplicate = -1 },
		func(p *descriptorDirectoryProof) { p.count = 0 }, func(p *descriptorDirectoryProof) { p.count = maxInheritedDescriptors + 1 },
		func(p *descriptorDirectoryProof) { p.eof = false }, func(p *descriptorDirectoryProof) { p.cloexec = false },
		func(p *descriptorDirectoryProof) { p.failed = true }, func(p *descriptorDirectoryProof) { p.opened = false },
		func(p *descriptorDirectoryProof) { p.closed = false }, func(p *descriptorDirectoryProof) { p.closeErrno = 9 },
	} {
		bad := good
		mutate(&bad)
		if bad.valid(4) {
			t.Fatal("incomplete/error native outcome admitted", bad)
		}
	}
	for _, names := range [][]uintptr{{0, 1, 2, 3, 4, 5, 5}, {0, 1, 2, 3, 4}, {0, 1, 2, 3, 5}, {0, 1, 2, 3, 4, 5, 2147483648}, make([]uintptr, maxInheritedDescriptors+1)} {
		if _, _, e := admitDescriptorNames(names, 4, 5); e == nil {
			t.Fatal("duplicate/missing/overflow names admitted")
		}
	}
}
