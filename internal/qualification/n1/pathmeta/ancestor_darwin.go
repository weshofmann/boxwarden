//go:build darwin && (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package pathmeta

import (
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"strings"
	"syscall"
	"time"
)

func ancestorTimes(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Mtimespec == y.Mtimespec && x.Ctimespec == y.Ctimespec && x.Flags == y.Flags
}
func (OSInspector) HasExactDenyDeleteACL(p string) (bool, error) {
	if contract.ProtectedAncestorMode(p) == 0 {
		return false, errors.New("qualification ancestor refused")
	}
	return denyDeleteACL(execx.OSRunner{MaxOutputBytes: 8 << 10, StrictStderr: true}, p)
}
func denyDeleteACL(runner execx.Runner, p string) (bool, error) {
	if runner == nil || contract.ProtectedAncestorMode(p) == 0 {
		return false, errors.New("qualification ancestor refused")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, e := runner.Run(ctx, execx.Command{Path: "/bin/ls", Args: []string{"-lde", p}, Env: []string{"LC_ALL=C", "LANG=C"}, Stdin: []byte{}})
	if e != nil || r.Truncated || r.StdoutTruncated || r.StderrTruncated || !r.StderrComplete || r.Stderr != "" {
		return false, errors.New("qualification ancestor refused")
	}
	return exactDenyDeleteOutput(p, r.Stdout), nil
}
func exactDenyDeleteOutput(p, s string) bool {
	mode := contract.ProtectedAncestorMode(p)
	if mode == 0 || len(s) > 8<<10 {
		return false
	}
	lines := strings.Split(s, "\n")
	if len(lines) != 3 || lines[1] != " 0: group:everyone deny delete" || lines[2] != "" {
		return false
	}
	fields := strings.Fields(lines[0])
	want := "drwx------+"
	if mode == 0750 {
		want = "drwxr-x---+"
	}
	if len(fields) < 9 || fields[0] != want {
		return false
	}
	// The header path may contain spaces; require the literal complete suffix.
	return strings.HasSuffix(lines[0], " "+p)
}
