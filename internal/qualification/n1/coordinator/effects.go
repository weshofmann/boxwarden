//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"os"
	"strings"
)

func role(i int) string {
	if i == 0 {
		return "control"
	}
	return "candidate"
}
func roleName(i int) string {
	if i == 0 {
		return config.N1ControlName
	}
	return config.N1CandidateName
}
func (e *engine) worker(ctx context.Context, i int, verb string, id worker.Identity, adjust func(*worker.Request), out any) error {
	if i < 0 || i > 1 || ctx.Err() != nil {
		return ErrRefused
	}
	if e.executable(i+2) != nil {
		return ErrRefused
	}
	r := worker.Request{Version: 1, Window: e.window, Identity: id}
	if adjust != nil {
		adjust(&r)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return ErrRefused
	}
	raw = append(raw, '\n')
	e.witness(contract.StaticFilePath(i+2), []string{verb})
	c, err := callChild(ctx, contract.StaticFilePath(i+2), []string{verb}, raw, 16384)
	if !c.closed {
		e.localDirty = true
	}
	if err != nil {
		return err
	}
	return decode(c.raw, out, 16384)
}
func (e *engine) cli(ctx context.Context, i int, args ...string) (childResult, error) {
	if e.executable(i) != nil {
		return childResult{exit: -1}, ErrRefused
	}
	a := []string{"--config", contract.EnrolledTarget(i), "--domain", config.N1Domain}
	a = append(a, args...)
	e.witness(contract.StaticFilePath(i), a)
	r, err := callChild(ctx, contract.StaticFilePath(i), a, nil, 16384)
	if !r.closed {
		e.localDirty = true
	}
	return r, err
}
func enroll(s fixed.StaticInputs) error {
	if mkdirPrivate(contract.StateRoot) != nil || fixed.CheckStateDirectory() != nil {
		return ErrRefused
	}
	for i := 0; i < 2; i++ {
		raw, err := privateRead(contract.StaticFilePath(10+i), 4096)
		if err != nil || contract.SHA(raw) != s.Lock.Files[10+i].SHA {
			return ErrRefused
		}
		c, err := config.Load(contract.StaticFilePath(10 + i))
		if err != nil {
			return ErrRefused
		}
		d, err := c.Domain(config.N1Domain)
		if err != nil || len(c.Domains()) != 1 || d.StateRoot != contract.StateRoot || d.WorkspaceStorage != nil {
			return ErrRefused
		}
		expected := hostidentity.StorageExpectation{ConfigPath: contract.EnrolledTarget(i), StateRoot: contract.StateRoot, MountPoint: "/Volumes/BoxwardenAlphaQualification", VolumeUUID: contract.VolumeUUID}
		enrolled, err := c.EnrolledCopy(config.N1Domain, config.WorkspaceStorage{MountPoint: expected.MountPoint, VolumeUUID: expected.VolumeUUID})
		if err != nil || contract.SHA(enrolled) != s.Lock.Configs[i] || hostidentity.CheckStorage(expected) != nil || hostidentity.WriteEnrolledConfig(expected, enrolled) != nil {
			return ErrRefused
		}
		received, err := privateRead(expected.ConfigPath, 4096)
		if err != nil || !bytes.Equal(received, enrolled) || contract.SHA(received) != s.Lock.Configs[i] {
			return ErrRefused
		}
	}
	return nil
}

type installRunner struct{}

func (installRunner) Run(ctx context.Context, c execx.Command) (execx.Result, error) {
	if c.Path != contract.SudoPath || len(c.Args) != 4 || c.Args[0] != "--" || c.Args[1] != contract.StaticFilePath(1) || c.Args[2] != "internal" || c.Args[3] != "host-install" || len(c.Env) != 0 {
		return execx.Result{}, ErrRefused
	}
	child, err := startChildEnvironment(ctx, c.Path, c.Args, c.Env, c.Stdin, 16384, nil)
	if err != nil {
		return execx.Result{}, err
	}
	r := child.join()
	if r.exit != 0 || !r.closed || r.stderr {
		err = ErrRefused
	}
	return execx.Result{Stdout: string(r.raw), Truncated: !r.closed}, err
}
func install(ctx context.Context, s fixed.StaticInputs) (hostx.RootInstallResult, error) {
	if fixed.CheckSudo() != nil {
		return hostx.RootInstallResult{}, ErrRefused
	}
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return hostx.RootInstallResult{}, ErrRefused
	}
	_, err = tty.WriteString("N1 approved window: attended installation of the exact diagnostic three-file tree. Authenticate only at sudo's Terminal prompt.\n")
	ce := tty.Close()
	if err != nil || ce != nil {
		return hostx.RootInstallResult{}, ErrRefused
	}
	c, err := config.LoadN1CurrentEnrollment()
	if err != nil {
		return hostx.RootInstallResult{}, ErrRefused
	}
	h, err := c.HostAdmission()
	if err != nil {
		return hostx.RootInstallResult{}, ErrRefused
	}
	r, err := hostx.InvokeRootInstall(ctx, installRunner{}, contract.StaticFilePath(1), hostx.InstallRequest{Version: hostx.InstallRequestVersion, SoftnetSource: contract.StaticFilePath(4), Tart: hostx.ToolIdentity{Path: h.Host.TartExecutable, Version: hostx.TartVersion, ExecutableSHA256: hostx.TartExecutableSHA256, ArchiveSHA256: hostx.TartArchiveSHA256}, TartHome: h.Host.TartHome})
	if err != nil || !r.Published || r.AlreadyInstalled || r.RefreshLoginSession {
		return r, ErrRefused
	}
	return r, nil
}
func (e *engine) start(ctx context.Context, i int) error {
	r, err := e.cli(ctx, i, "session", "start", roleName(i))
	expected := fmt.Sprintf("domain: n1qualification\nsession: %s\nstate: running\nreadiness: ready\n", roleName(i))
	if err != nil || string(r.raw) != expected {
		return ErrRefused
	}
	id := worker.Identity{Session: e.fresh[i].Record.ID, Backend: e.fresh[i].Record.Backend.ObjectID}
	var o worker.RuntimeObservation
	if e.worker(ctx, i, "discover", id, nil, &o) != nil || admitRuntime(o, i, id) != nil {
		return ErrRefused
	}
	e.pair[i] = o
	var g sshx.N1InspectResult
	phase := sshx.N1GuestInitial
	if o.Identity.Generation != e.fresh[i].Record.StartGeneration && e.fresh[i].Record.StartGeneration != "" {
		phase = sshx.N1GuestFinal
	}
	// Explicit final inspector is called by the restart phase. Initial admission
	// happens only on the first start, while the clone still has the old helper.
	if e.fresh[i].Record.StartGeneration == "" {
		if e.worker(ctx, i, "inspect-guest", o.Identity, func(r *worker.Request) { r.Phase = phase }, &g) != nil || g.Phase != phase {
			return ErrRefused
		}
		e.fresh[i].Record.StartGeneration = o.Identity.Generation
	}
	return nil
}
func (e *engine) guestFinal(ctx context.Context, i int) error {
	var g sshx.N1InspectResult
	if e.worker(ctx, i, "inspect-guest", e.pair[i].Identity, func(r *worker.Request) { r.Phase = sshx.N1GuestFinal }, &g) != nil || g.Phase != sshx.N1GuestFinal || g.Installed != 9 || g.DiagnosticNamespace != "absent" {
		return ErrRefused
	}
	return nil
}

type stageReceipt = sshx.N1StageResult
type lifecycleObservation struct {
	Version       int             `json:"version"`
	Role          string          `json:"role"`
	Name          string          `json:"name"`
	Identity      worker.Identity `json:"identity"`
	BackendState  string          `json:"backend_state"`
	RuntimeAbsent bool            `json:"runtime_absent"`
	RecordAbsent  bool            `json:"record_absent"`
}

func (e *engine) stop(ctx context.Context, i int) error {
	old := e.pair[i]
	var before lifecycleObservation
	if e.worker(ctx, i, "inspect-running", old.Identity, nil, &before) != nil || before.Version != 1 || before.Role != role(i) || before.Name != roleName(i) || before.Identity != old.Identity || before.BackendState != "running" || before.RuntimeAbsent || before.RecordAbsent {
		return ErrRefused
	}
	r, err := e.cli(ctx, i, "session", "stop", roleName(i))
	if err != nil || !r.closed || !stopAcknowledged(r.raw, roleName(i)) {
		return ErrRefused
	}
	record, err := session.LoadRecord(contract.StateRoot, config.N1Domain, roleName(i))
	if err != nil || record.ID != old.Identity.Session || record.Backend.ObjectID != old.Identity.Backend || record.IntendedState != session.StateStopped || record.StartGeneration != "" || !strings.HasSuffix(string(r.raw), "stop-outcome: "+record.Readiness.Diagnostic+"\n") {
		return ErrRefused
	}
	var proof lifecycleObservation
	if e.worker(ctx, i, "inspect-stopped", old.Identity, nil, &proof) != nil || proof.Version != 1 || proof.Role != role(i) || proof.Name != roleName(i) || proof.Identity != old.Identity || proof.BackendState != "stopped" || !proof.RuntimeAbsent || proof.RecordAbsent {
		return ErrRefused
	}
	return nil
}
func stopAcknowledged(raw []byte, name string) bool {
	prefix := fmt.Sprintf("domain: n1qualification\nsession: %s\nstate: stopped\nreadiness: not_ready\nstop-outcome: request=", name)
	for _, request := range []string{"unrequested", "guest_accepted", "tart_fallback", "tart_only"} {
		for _, forced := range []string{"true", "false"} {
			if string(raw) == prefix+request+" forced="+forced+" workspace_cleanliness=unverified\n" {
				return true
			}
		}
	}
	return false
}
func (e *engine) delete(ctx context.Context, i int) error {
	r, err := e.cli(ctx, i, "session", "delete", roleName(i))
	if err != nil || !r.closed || string(r.raw) != fmt.Sprintf("domain: n1qualification\nsession: %s\nstate: deleted\nworkspaces: retained\n", roleName(i)) {
		return ErrRefused
	}
	var proof lifecycleObservation
	if e.worker(ctx, i, "inspect-deleted", e.pair[i].Identity, nil, &proof) != nil || proof.Version != 1 || proof.Role != role(i) || proof.Name != roleName(i) || proof.Identity != e.pair[i].Identity || proof.BackendState != "missing" || !proof.RuntimeAbsent || !proof.RecordAbsent {
		return ErrRefused
	}
	return nil
}

func (e *engine) executable(index int) error {
	if index < 0 || index > 3 || len(e.static.Lock.Files) < 4 {
		return ErrRefused
	}
	raw, err := privateReadMode(contract.StaticFilePath(index), 128<<20, 0500)
	if err != nil || contract.SHA(raw) != e.static.Lock.Files[index].SHA {
		return ErrRefused
	}
	return nil
}
