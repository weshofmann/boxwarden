package guestproto

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"os/user"
	"path"
	"strconv"
	"syscall"
	"time"
)

const actionWallTimeout = 10 * time.Minute

// ActionExecutor runs a validated argv vector in the disposable guest. It
// never receives a host path or a host credential.
type ActionExecutor interface {
	Run(context.Context, []string) error
}

// WorkstationActionExecutor runs as the disposable workstation account, even
// though the fixed helper runs as root to inspect root-owned binding state.
// The guest account has its normal passwordless sudo rights inside the VM.
type WorkstationActionExecutor struct{}

func (WorkstationActionExecutor) Run(ctx context.Context, argv []string) error {
	if len(argv) == 0 || !path.IsAbs(argv[0]) || path.Clean(argv[0]) != argv[0] {
		return fmt.Errorf("action executable must be an absolute canonical guest path")
	}
	account, err := user.Lookup("boxwarden")
	if err != nil || account.Username != "boxwarden" || account.HomeDir != "/home/boxwarden" {
		return fmt.Errorf("workstation account is unavailable or changed: %v", err)
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil || uid == 0 {
		return fmt.Errorf("invalid workstation UID")
	}
	gid, err := strconv.ParseUint(account.Gid, 10, 32)
	if err != nil || gid == 0 {
		return fmt.Errorf("invalid workstation GID")
	}
	groupIDs, err := account.GroupIds()
	if err != nil {
		return fmt.Errorf("inspect workstation groups: %w", err)
	}
	groups := make([]uint32, 0, len(groupIDs))
	for _, raw := range groupIDs {
		id, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid workstation supplemental group")
		}
		groups = append(groups, uint32(id))
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir = account.HomeDir
	command.Env = []string{
		"HOME=/home/boxwarden",
		"USER=boxwarden",
		"LOGNAME=boxwarden",
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"LANG=C.UTF-8",
	}
	command.Stdin = nil
	command.Stdout, command.Stderr = io.Discard, io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Credential: &syscall.Credential{
			Uid: uint32(uid), Gid: uint32(gid), Groups: groups,
		},
	}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	if err := command.Run(); err != nil {
		return fmt.Errorf("guest action failed: %w", err)
	}
	return nil
}

// ExecuteAction claims before running, then publishes a success receipt. A
// failed command or interrupted publication leaves the claim indeterminate;
// the same attempt is never silently retried. Desktop launch needs a separate
// bounded session environment and remains disabled here.
func (b *Bootstrapper) ExecuteAction(ctx context.Context, request ActionRequest) (ActionReceipt, error) {
	if b == nil || b.ActionExecutor == nil {
		return ActionReceipt{}, fmt.Errorf("action executor is required")
	}
	if err := ctx.Err(); err != nil {
		return ActionReceipt{}, err
	}
	if err := request.Validate(); err != nil {
		return ActionReceipt{}, err
	}
	if request.ActionPhase == "launch" {
		return ActionReceipt{}, fmt.Errorf("desktop launch action is not yet supported")
	}
	operationCtx, cancel := context.WithTimeout(ctx, actionWallTimeout)
	defer cancel()
	prior, err := b.ClaimAction(request)
	if err != nil {
		return ActionReceipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	if err := b.ActionExecutor.Run(operationCtx, request.Argv); err != nil {
		return ActionReceipt{}, err
	}
	_, digest, err := EncodeActionRequest(request)
	if err != nil {
		return ActionReceipt{}, err
	}
	receipt := ActionReceipt{
		Version: Version, Association: request.Association,
		Generation: request.Generation, RecipeDigest: request.RecipeDigest,
		ActionID: request.ActionID, ActionPhase: request.ActionPhase,
		AttemptID: request.AttemptID, RequestSHA256: digest, State: "succeeded",
	}
	if err := b.PublishActionSuccess(request, receipt); err != nil {
		return ActionReceipt{}, err
	}
	return receipt, nil
}
