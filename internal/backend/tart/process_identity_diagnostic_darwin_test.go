//go:build n1diagnostic && !n1candidate && darwin && cgo

package tart

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"os"
	"testing"
)

func TestNativeDiagnosticCorrelationUsesOnlyActualRetainedChild(t *testing.T) {
	out, e := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer out.Close()
	h, e := startDiagnosticProcess(t.Context(), processSpec{path: "/bin/sleep", args: []string{"30"}, dir: t.TempDir(), env: []string{}}, out)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { h.Stop(context.Background()); h.Wait(context.Background()) }()
	typed := h.(*osProcessHandle)
	first, e := typed.RetainedDiagnosticProcess()
	if e != nil || first.PID != uint32(typed.process.Pid) || !first.Valid() {
		t.Fatal("native retained child", e)
	}
	second, e := typed.RetainedDiagnosticProcess()
	if e != nil || first != second {
		t.Fatal("unstable child birth", e)
	}
	self, e := networkdiag.CurrentOwnerProcess()
	if e != nil || self.PID != uint32(os.Getpid()) || self.PID == first.PID {
		t.Fatal("owner correlation", e)
	}
	h.Stop(t.Context())
	h.Wait(t.Context())
	if _, e := typed.RetainedDiagnosticProcess(); e == nil {
		t.Fatal("reaped child correlation admitted")
	}
}
