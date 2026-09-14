//go:build darwin

package hostx

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

const productionToolchainRoot = "/Library/Boxwarden"
const newGroupVisibilityReads = 11
const newGroupVisibilityPause = 100 * time.Millisecond
const newGroupVisibilityBudget = time.Second

// RunRootHostInstall is the hidden root-phase entry point. The public CLI must
// dispatch to it only for the exact `internal host-install` argv and pass its
// bounded stdin/stdout verbatim.
func RunRootHostInstall(ctx context.Context, input []byte) ([]byte, error) {
	platform := NewOSDoctorInspector().Platform()
	if !qualifiedPlatformFact(platform) {
		return nil, ErrUnsupportedPlatform
	}
	request, err := DecodeInstallRequest(input)
	if err != nil {
		return nil, err
	}
	runner := execx.OSRunner{MaxOutputBytes: 16 << 10}
	result, err := (RootInstaller{
		Identity: newOSRootIdentity(),
		Groups:   darwinGroupManager{runner: runner},
		Publisher: RootedPublisher{
			Root: productionToolchainRoot, ACL: OSACLInspector{},
		},
	}).Install(ctx, request)
	if err != nil {
		return nil, err
	}
	return EncodeRootInstallResult(result)
}

type darwinGroupManager struct {
	runner      execx.Runner
	lookupGroup func(string) (*user.Group, error)
	pause       func(time.Duration)
	now         func() time.Time
}

func (m darwinGroupManager) Ensure(caller Caller, name string) (Group, bool, error) {
	if name != OperatorGroupName || caller.UID <= 0 || caller.Name == "" {
		return Group{}, false, fmt.Errorf("invalid operator group request")
	}
	changed := false
	lookupGroup := m.lookupGroup
	if lookupGroup == nil {
		lookupGroup = user.LookupGroup
	}
	_, lookupErr := lookupGroup(name)
	if lookupErr != nil {
		if _, ok := lookupErr.(user.UnknownGroupError); !ok {
			return Group{}, false, lookupErr
		}
		if err := m.run("/usr/sbin/dseditgroup", "-o", "create", "-n", "/Local/Default", name); err != nil {
			return Group{}, false, fmt.Errorf("create dedicated operator group: %w", err)
		}
		changed = true
	}
	clock := m.now
	if clock == nil {
		clock = time.Now
	}
	deadline := clock().Add(newGroupVisibilityBudget)
	group, err := inspectExactLocalOperatorGroup(m.runner, Operator{UID: caller.UID, Name: caller.Name, Home: caller.Home}, name, true)
	if changed {
		pause := m.pause
		if pause == nil {
			pause = time.Sleep
		}
		for reads := 1; errors.Is(err, errLocalOperatorGroupNotVisible); reads++ {
			if reads == newGroupVisibilityReads || !clock().Before(deadline) {
				return Group{}, false, fmt.Errorf("new local operator group did not become visible within bounded exact reads: %w", err)
			}
			delay := newGroupVisibilityPause
			if remaining := deadline.Sub(clock()); remaining < delay {
				delay = remaining
			}
			pause(delay)
			if !clock().Before(deadline) {
				return Group{}, false, fmt.Errorf("new local operator group did not become visible within bounded exact reads: %w", err)
			}
			group, err = inspectExactLocalOperatorGroup(m.runner, Operator{UID: caller.UID, Name: caller.Name, Home: caller.Home}, name, true)
		}
	}
	if err != nil {
		return Group{}, false, err
	}
	if len(group.Members) == 0 {
		if err := m.run("/usr/sbin/dseditgroup", "-o", "edit", "-n", "/Local/Default", "-a", caller.Name, "-t", "user", name); err != nil {
			return Group{}, false, fmt.Errorf("add exact trusted operator: %w", err)
		}
		changed = true
		group, err = inspectExactLocalOperatorGroup(m.runner, Operator{UID: caller.UID, Name: caller.Name, Home: caller.Home}, name, false)
		if err != nil {
			return Group{}, false, err
		}
	}
	if len(group.Members) != 1 || group.Members[0] != caller.UID {
		return Group{}, false, fmt.Errorf("operator group membership did not converge exactly")
	}
	return group, changed, nil
}

func (m darwinGroupManager) run(path string, args ...string) error {
	if m.runner == nil {
		return fmt.Errorf("directory-service runner is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := m.runner.Run(ctx, execx.Command{Path: path, Args: args, Env: []string{"LC_ALL=C", "LANG=C"}})
	if result.Truncated {
		return fmt.Errorf("directory-service command output truncated")
	}
	if err != nil {
		return fmt.Errorf("directory-service command failed: %w", err)
	}
	return nil
}
