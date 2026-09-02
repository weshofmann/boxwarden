//go:build darwin

package hostx

import (
	"context"
	"fmt"
	"os/user"
	"sort"
	"strconv"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

const productionToolchainRoot = "/Library/Boxwarden"

// RunRootHostInstall is the hidden root-phase entry point. The public CLI must
// dispatch to it only for the exact `internal host-install` argv and pass its
// bounded stdin/stdout verbatim.
func RunRootHostInstall(ctx context.Context, input []byte) ([]byte, error) {
	platform := NewOSDoctorInspector().Platform()
	if platform.OS != QualifiedPlatform || platform.Arch != QualifiedArch || platform.Release != QualifiedMacOS {
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

type darwinGroupManager struct{ runner execx.Runner }

func (m darwinGroupManager) Ensure(caller Caller, name string) (Group, bool, error) {
	if name != OperatorGroupName || caller.UID <= 0 || caller.Name == "" {
		return Group{}, false, fmt.Errorf("invalid operator group request")
	}
	changed := false
	entry, lookupErr := user.LookupGroup(name)
	if lookupErr != nil {
		if _, ok := lookupErr.(user.UnknownGroupError); !ok {
			return Group{}, false, lookupErr
		}
		if err := m.run("/usr/sbin/dseditgroup", "-o", "create", "-n", "/Local/Default", name); err != nil {
			return Group{}, false, fmt.Errorf("create dedicated operator group: %w", err)
		}
		changed = true
		entry, lookupErr = user.LookupGroup(name)
		if lookupErr != nil {
			return Group{}, false, fmt.Errorf("lookup newly created operator group: %w", lookupErr)
		}
	}
	gid, err := strconv.Atoi(entry.Gid)
	if err != nil || gid < 0 {
		return Group{}, false, fmt.Errorf("operator group has invalid gid")
	}
	members, err := lookupDirectGroupMembers(m.runner, name)
	if err != nil {
		return Group{}, false, err
	}
	if len(members) > 1 || len(members) == 1 && members[0] != caller.UID {
		return Group{}, false, fmt.Errorf("existing operator group has unexpected membership")
	}
	if len(members) == 0 {
		if err := m.run("/usr/sbin/dseditgroup", "-o", "edit", "-a", caller.Name, "-t", "user", name); err != nil {
			return Group{}, false, fmt.Errorf("add exact trusted operator: %w", err)
		}
		changed = true
		members, err = lookupDirectGroupMembers(m.runner, name)
		if err != nil {
			return Group{}, false, err
		}
	}
	sort.Ints(members)
	if len(members) != 1 || members[0] != caller.UID {
		return Group{}, false, fmt.Errorf("operator group membership did not converge exactly")
	}
	return Group{ID: gid, Name: name, Members: members}, changed, nil
}

func (m darwinGroupManager) run(path string, args ...string) error {
	if m.runner == nil {
		return fmt.Errorf("directory-service runner is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := m.runner.Run(ctx, execx.Command{Path: path, Args: args, Env: []string{"LC_ALL=C", "LANG=C"}})
	if err != nil || result.Truncated {
		return fmt.Errorf("directory-service command failed")
	}
	return nil
}
