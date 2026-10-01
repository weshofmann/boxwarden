//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"testing"
)

// Calls the actual C count/error boundary with injected scalars only; never
// invokes proc_listallpids, proc_pidinfo, proc_pidpath or current-host census.
func TestNativeCountContractInjectedOnly(t *testing.T) {
	for _, x := range []struct{ count, code, want int }{{1, 0, 1}, {8192, 0, 8192}, {0, 0, -1}, {-1, 0, -1}, {8193, 0, -1}, {1, 13, -1}} {
		if got := checkedNativeCount(x.count, x.code); got != x.want {
			t.Fatal(x, got)
		}
	}
}

func validNativeKernelBSD() nativeBSD {
	b := nativeBSD{Selected: kernelObservation{Status: 2, Flags: 17}, Seconds: 1, Microseconds: 23, Result: nativeBSDSize}
	copy(b.Comm[:], "kernel_task")
	copy(b.Name[:], "kernel_task")
	return b
}
func TestNativeKernelContractInjectedOnly(t *testing.T) {
	for _, mode := range []string{"positive", "size", "error", "pid", "ppid", "uid", "gid", "ruid", "rgid", "suid", "sgid", "status", "flags", "comm", "name", "comm-unterminated", "name-unterminated", "usec", "overflow", "zero-birth", "path", "path-result", "path-error", "path-hidden-byte", "second-drift", "second-size", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls, paths := 0, 0
			c := kernelCalls{bsd: func(pid int) nativeBSD {
				if pid != 0 {
					t.Fatal("nonzero kernel lookup")
				}
				calls++
				b := validNativeKernelBSD()
				switch mode {
				case "size":
					b.Result--
				case "error":
					b.Code = 13
				case "pid":
					b.Selected.PID = 1
				case "ppid":
					b.Selected.PPID = 1
				case "uid":
					b.Selected.UID = 1
				case "gid":
					b.Selected.GID = 1
				case "ruid":
					b.Selected.RUID = 1
				case "rgid":
					b.Selected.RGID = 1
				case "suid":
					b.Selected.SUID = 1
				case "sgid":
					b.Selected.SGID = 1
				case "status":
					b.Selected.Status = 5
				case "flags":
					b.Selected.Flags = 1
				case "comm":
					b.Comm[0] = 'x'
				case "name":
					b.Name[0] = 'x'
				case "comm-unterminated":
					for i := range b.Comm {
						b.Comm[i] = 'x'
					}
				case "name-unterminated":
					for i := range b.Name {
						b.Name[i] = 'x'
					}
				case "usec":
					b.Microseconds = 1000000
				case "overflow":
					b.Seconds = ^uint64(0)
				case "zero-birth":
					b.Seconds = 0
					b.Microseconds = 0
				case "second-drift":
					if calls == 2 {
						b.Microseconds++
					}
				case "second-size":
					if calls == 2 {
						b.Result--
					}
				case "cancel":
					cancel()
				}
				return b
			}, path: func(pid int) nativePath {
				if pid != 0 {
					t.Fatal("nonzero path")
				}
				paths++
				p := nativePath{Code: 3}
				switch mode {
				case "path":
					p.Buffer[0] = 'x'
				case "path-result":
					p.Result = 1
				case "path-error":
					p.Code = 13
				case "path-hidden-byte":
					p.Buffer[len(p.Buffer)-1] = 1
				}
				return p
			}}
			p, e := observeKernel(ctx, c)
			if mode == "positive" {
				want := kernelObservation{Status: 2, Flags: 17, Comm: "kernel_task", Name: "kernel_task", Birth: 1000023}
				if e != nil || p.Kernel != want || p.Birth != 1000023 || p.PID != 0 || p.Unique != 0 || p.Path != "" || p.SHA != "" || p.Device != 0 || p.Inode != 0 || p.QualificationSHA != "" || p.Kind != "kernel" || calls != 2 || paths != 1 {
					t.Fatal(p, e, calls, paths)
				}
			} else if e == nil {
				t.Fatal("invalid kernel accepted", mode, p)
			}
		})
	}
}
func TestNativeSnapshotCannotFabricateKernelExecutable(t *testing.T) {
	p := fixtureKernel()
	p.SHA = contract.SoftnetSHA
	if _, e := nativeSnapshot(t.Context(), []int{0}, func(int) (process, error) { return p, nil }); e == nil {
		t.Fatal("kernel with executable digest admitted")
	}
}
func TestNativeSnapshotZeroAndInclusiveBoundInjectedOnly(t *testing.T) {
	for _, mode := range []string{"positive", "missing", "duplicate", "negative", "fake-positive", "error", "cancel", "8192", "8193"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pids := []int{0, 1}
			switch mode {
			case "missing":
				pids = []int{1}
			case "duplicate":
				pids = []int{0, 0}
			case "negative":
				pids = []int{0, -1}
			case "8192", "8193":
				n := 8192
				if mode == "8193" {
					n++
				}
				pids = make([]int, n)
				for i := range pids {
					pids[i] = i
				}
			case "cancel":
				cancel()
			}
			out, e := nativeSnapshot(ctx, pids, func(pid int) (process, error) {
				if mode == "error" {
					return process{}, ErrRefused
				}
				if pid == 0 {
					if mode == "fake-positive" {
						p := fixtureKernel()
						p.PID = 1
						return p, nil
					}
					return fixtureKernel(), nil
				}
				return process{PID: pid}, nil
			})
			if mode == "positive" || mode == "8192" {
				if e != nil || len(out) != len(pids) {
					t.Fatal(e, len(out))
				}
			} else if e == nil {
				t.Fatal("uncertain snapshot accepted")
			}
		})
	}
}
