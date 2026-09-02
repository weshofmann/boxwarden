//go:build darwin

package hostx

import (
	"context"
	"fmt"
	"os/user"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func lookupDirectGroupMembers(runner execx.Runner, name string) ([]int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/dscl", Args: []string{"/Local/Default", "-read", "/Groups/" + name, "GroupMembership"}, Env: []string{"LC_ALL=C", "LANG=C"}})
	if err != nil {
		diagnostic := result.Stdout + result.Stderr
		if strings.Contains(diagnostic, "No such key: GroupMembership") || strings.Contains(diagnostic, "eDSAttributeNotFound: GroupMembership") {
			return []int{}, nil
		}
		return nil, fmt.Errorf("inspect direct group membership")
	}
	if result.Truncated {
		return nil, fmt.Errorf("inspect direct group membership")
	}
	fields := strings.Fields(result.Stdout)
	if len(fields) == 0 || strings.TrimSuffix(fields[0], ":") != "GroupMembership" {
		return nil, fmt.Errorf("malformed directory group membership")
	}
	var members []int
	for _, name := range fields[1:] {
		entry, err := user.Lookup(name)
		if err != nil {
			return nil, err
		}
		uid, err := strconv.Atoi(entry.Uid)
		if err != nil {
			return nil, err
		}
		members = append(members, uid)
	}
	sort.Ints(members)
	return members, nil
}
