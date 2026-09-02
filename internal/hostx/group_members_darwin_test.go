//go:build darwin

package hostx

import (
	"context"
	"errors"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestLookupDirectGroupMembersTreatsOnlyMissingMembershipAttributeAsEmpty(t *testing.T) {
	for name, result := range map[string]execx.Result{
		"named missing key":      {Stderr: "No such key: GroupMembership\n"},
		"directory missing attr": {Stderr: "eDSAttributeNotFound: GroupMembership\n"},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &groupLookupRunnerFake{result: result, err: errors.New("exit status 1")}
			members, err := lookupDirectGroupMembers(runner, OperatorGroupName)
			if err != nil || len(members) != 0 {
				t.Fatalf("lookupDirectGroupMembers() = %v, %v; want empty", members, err)
			}
			if runner.command.Path != "/usr/bin/dscl" || len(runner.command.Args) < 1 || runner.command.Args[0] != "/Local/Default" || runner.command.Env == nil {
				t.Fatalf("directory lookup command = %#v, want exact local node and closed environment", runner.command)
			}
		})
	}
	runner := &groupLookupRunnerFake{result: execx.Result{Stderr: "permission denied\n"}, err: errors.New("exit status 1")}
	if _, err := lookupDirectGroupMembers(runner, OperatorGroupName); err == nil {
		t.Fatal("lookupDirectGroupMembers(unrelated failure) error = nil")
	}
}

type groupLookupRunnerFake struct {
	result  execx.Result
	err     error
	command execx.Command
}

func (f *groupLookupRunnerFake) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	f.command = command
	return f.result, f.err
}
