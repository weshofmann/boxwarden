//go:build linux

package hostx

import (
	"fmt"
	"os/user"
	"sort"
	"strconv"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func lookupDirectGroupMembers(_ execx.Runner, name string) ([]int, error) {
	group, err := user.LookupGroup(name)
	if err != nil {
		return nil, err
	}
	var members []int
	users, err := user.LookupId("0")
	if err != nil {
		return nil, fmt.Errorf("Linux group enumeration unavailable: %w", err)
	}
	ids, err := users.GroupIds()
	if err == nil {
		for _, id := range ids {
			if id == group.Gid {
				uid, _ := strconv.Atoi(users.Uid)
				members = append(members, uid)
			}
		}
	}
	sort.Ints(members)
	return members, nil
}
