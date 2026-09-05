package hostx

import (
	"context"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestOSDoctorCommandInspectionHasIndependentBound(t *testing.T) {
	inspector := &osDoctorInspector{runner: blockingRunner{}, acl: noACLInspector{}, timeout: 20 * time.Millisecond}
	started := time.Now()
	if _, err := inspector.CommandOutput("/usr/bin/ssh", "-V"); err == nil {
		t.Fatal("CommandOutput() error = nil, want timeout")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("CommandOutput() took %s, want bounded completion", elapsed)
	}
}

type blockingRunner struct{}

func (blockingRunner) Run(ctx context.Context, _ execx.Command) (execx.Result, error) {
	<-ctx.Done()
	return execx.Result{}, ctx.Err()
}
