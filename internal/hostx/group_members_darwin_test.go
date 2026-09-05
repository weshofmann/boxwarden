//go:build darwin

package hostx

import (
	"context"
	"errors"
	"fmt"
	"os/user"
	"reflect"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestInspectExactLocalOperatorGroupAcceptsOnlyExhaustiveCallerBinding(t *testing.T) {
	for name, primaryUsers := range map[string]string{
		"supplementary only": "daemon 1\nwes 20\n",
		"caller primary gid": "wes 701\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner(primaryUsers)
			caller := Operator{UID: 501, Name: "wes", Home: "/Users/wes"}
			group, err := inspectExactLocalOperatorGroup(runner, caller, OperatorGroupName, false)
			if err != nil || !reflect.DeepEqual(group, Group{ID: 701, Name: OperatorGroupName, Members: []int{501}}) {
				t.Fatalf("inspectExactLocalOperatorGroup() = %#v, %v", group, err)
			}
			if len(runner.commands) != 4 {
				t.Fatalf("directory commands = %d, want four exhaustive queries", len(runner.commands))
			}
			for index, command := range runner.commands {
				wantNode := "/Local/Default"
				if index >= 2 {
					wantNode = "/Search"
				}
				if command.Path != "/usr/bin/dscl" || len(command.Args) < 2 || command.Args[0] != wantNode || !reflect.DeepEqual(command.Env, []string{"LC_ALL=C", "LANG=C"}) {
					t.Fatalf("directory command = %#v, want fixed bounded argv on %s with closed environment", command, wantNode)
				}
			}
			if !reflect.DeepEqual(runner.commands[2].Args, []string{"/Search", "-list", "/Users", "PrimaryGroupID"}) || !reflect.DeepEqual(runner.commands[3].Args, []string{"/Search", "-list", "/Groups", "PrimaryGroupID"}) {
				t.Fatalf("collision scans = %#v, %#v; want exact search-policy PrimaryGroupID inventories", runner.commands[2].Args, runner.commands[3].Args)
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupAcceptsSignedNonTargetPrimaryGIDs(t *testing.T) {
	runner := exactGroupSnapshotRunner("nobody -2\nfuture-system -3\nwes 20\n")
	runner.set(groupGIDListArgs(), "nobody -2\nnogroup -1\nstaff 20\nboxwarden-operators 701\n")

	group, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false)
	if err != nil || !reflect.DeepEqual(group, Group{ID: 701, Name: OperatorGroupName, Members: []int{501}}) {
		t.Fatalf("inspectExactLocalOperatorGroup() = %#v, %v", group, err)
	}
}

func TestInspectExactLocalOperatorGroupRejectsTargetGIDCollisionsAlongsideSignedSentinels(t *testing.T) {
	for name, mutate := range map[string]func(*scriptedGroupRunner){
		"other user": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "nobody -2\nfuture-system -3\nother 701\nwes 20\n")
			r.set(groupGIDListArgs(), "nobody -2\nnogroup -1\nstaff 20\nboxwarden-operators 701\n")
		},
		"group alias": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "nobody -2\nfuture-system -3\nwes 20\n")
			r.set(groupGIDListArgs(), "nobody -2\nnogroup -1\nalias 701\nstaff 20\nboxwarden-operators 701\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			mutate(runner)
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want target gid collision refusal")
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupAcceptsCallerAmongUniqueRecordNameAliases(t *testing.T) {
	for name, recordNames := range map[string]string{
		"observed primary name followed by Apple ID alias shape": "wes com.apple.idms.appleid.prd.example-local-alias",
		"caller name not first":                                  "com.apple.idms.appleid.prd.example-local-alias wes",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			runner.set(userReadArgs(), "GeneratedUID: CALLER-GUID\nRecordName: "+recordNames+"\nUniqueID: 501\n")
			group, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false)
			if err != nil || !reflect.DeepEqual(group, Group{ID: 701, Name: OperatorGroupName, Members: []int{501}}) {
				t.Fatalf("inspectExactLocalOperatorGroup() = %#v, %v", group, err)
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupRejectsAmbiguousCallerRecordNameEvidence(t *testing.T) {
	for name, recordNames := range map[string]string{
		"caller absent":         "other com.apple.idms.appleid.prd.example-local-alias",
		"duplicate caller":      "wes wes",
		"duplicate other alias": "wes alias alias",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			runner.set(userReadArgs(), "GeneratedUID: CALLER-GUID\nRecordName: "+recordNames+"\nUniqueID: 501\n")
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want ambiguous caller record refusal")
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupRejectsNonExactCallerUIDAndGUIDEvidence(t *testing.T) {
	for name, userRecord := range map[string]string{
		"missing UID":    "GeneratedUID: CALLER-GUID\nRecordName: wes\n",
		"multiple UIDs":  "GeneratedUID: CALLER-GUID\nRecordName: wes\nUniqueID: 501 502\n",
		"wrong UID":      "GeneratedUID: CALLER-GUID\nRecordName: wes\nUniqueID: 502\n",
		"missing GUID":   "RecordName: wes\nUniqueID: 501\n",
		"multiple GUIDs": "GeneratedUID: CALLER-GUID OTHER-GUID\nRecordName: wes\nUniqueID: 501\n",
		"empty GUID":     "GeneratedUID:\nRecordName: wes\nUniqueID: 501\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			runner.set(userReadArgs(), userRecord)
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want non-exact caller UID/GUID refusal")
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupRejectsEveryAlternateMembershipPath(t *testing.T) {
	for name, mutate := range map[string]func(*scriptedGroupRunner){
		"extra named member": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes other\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"extra GUID member": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID OTHER-GUID\nGroupMembership: wes\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"nested group": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nNestedGroups: NESTED-GUID\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"other primary gid user": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "other 701\nwes 20\n")
		},
		"aliased group gid": func(r *scriptedGroupRunner) {
			r.set(groupGIDListArgs(), "alias 701\nboxwarden-operators 701\n")
		},
		"wrong direct name": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: other\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"wrong caller GUID": func(r *scriptedGroupRunner) {
			r.set(userReadArgs(), "GeneratedUID: OTHER-GUID\nRecordName: wes\nUniqueID: 501\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			mutate(runner)
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want alternate membership refusal")
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupRejectsIncompleteOrMalformedExhaustiveListings(t *testing.T) {
	for name, mutate := range map[string]func(*scriptedGroupRunner){
		"caller absent from user listing": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "daemon 1\n")
		},
		"group absent from group listing": func(r *scriptedGroupRunner) {
			r.set(groupGIDListArgs(), "staff 20\n")
		},
		"missing listed value": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "wes\n")
		},
		"nonnumeric listed value": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "wes twenty\n")
		},
		"duplicate listed record": func(r *scriptedGroupRunner) {
			r.set(primaryUserListArgs(), "wes 20\nwes 20\n")
		},
		"truncated user listing": func(r *scriptedGroupRunner) {
			r.truncated[scriptedKey(primaryUserListArgs())] = true
		},
		"truncated group listing": func(r *scriptedGroupRunner) {
			r.truncated[scriptedKey(groupGIDListArgs())] = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("wes 20\n")
			mutate(runner)
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want incomplete exhaustive listing refusal")
			}
		})
	}
}

func TestListDirectoryPrimaryGIDsRejectsMalformedEvidence(t *testing.T) {
	for name, output := range map[string]string{
		"bare record":                  "wes\n",
		"nonnumeric gid":               "wes twenty\n",
		"repeated negative sign":       "wes --2\n",
		"repeated positive sign":       "wes ++2\n",
		"extra field":                  "wes 20 extra\n",
		"duplicate record":             "wes 20\nwes 20\n",
		"positive native-int overflow": "wes 999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999\n",
		"negative native-int overflow": "wes -999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			runner.set(primaryUserListArgs(), output)
			if _, err := listDirectoryPrimaryGIDs(runner, primaryUserListArgs()); err == nil {
				t.Fatal("listDirectoryPrimaryGIDs() error = nil, want malformed inventory refusal")
			}
		})
	}

	t.Run("truncated output", func(t *testing.T) {
		runner := exactGroupSnapshotRunner("")
		runner.truncated[scriptedKey(primaryUserListArgs())] = true
		if _, err := listDirectoryPrimaryGIDs(runner, primaryUserListArgs()); err == nil {
			t.Fatal("listDirectoryPrimaryGIDs() error = nil, want truncated inventory refusal")
		}
	})
}

func TestDarwinGroupManagerRefusesMalformedInventoryBeforeMutation(t *testing.T) {
	for name, output := range map[string]string{
		"bare record":                  "wes\n",
		"nonnumeric gid":               "wes twenty\n",
		"repeated signs":               "wes --2\n",
		"extra field":                  "wes 20 extra\n",
		"duplicate record":             "wes 20\nwes 20\n",
		"positive native-int overflow": "wes 999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999\n",
		"negative native-int overflow": "wes -999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999999\n",
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			runner.set(groupReadArgs(), "PrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
			runner.set(primaryUserListArgs(), output)
			manager := darwinGroupManager{runner: runner, lookupGroup: func(string) (*user.Group, error) {
				return &user.Group{Gid: "701", Name: OperatorGroupName}, nil
			}}
			if _, _, err := manager.Ensure(Caller{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName); err == nil {
				t.Fatal("Ensure() error = nil, want malformed inventory refusal")
			}
			if runner.mutations != 0 {
				t.Fatalf("directory mutations = %d, want zero before malformed inventory refusal", runner.mutations)
			}
		})
	}

	t.Run("truncated output", func(t *testing.T) {
		runner := exactGroupSnapshotRunner("")
		runner.set(groupReadArgs(), "PrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		runner.truncated[scriptedKey(primaryUserListArgs())] = true
		manager := darwinGroupManager{runner: runner, lookupGroup: func(string) (*user.Group, error) {
			return &user.Group{Gid: "701", Name: OperatorGroupName}, nil
		}}
		if _, _, err := manager.Ensure(Caller{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName); err == nil {
			t.Fatal("Ensure() error = nil, want truncated inventory refusal")
		}
		if runner.mutations != 0 {
			t.Fatalf("directory mutations = %d, want zero before truncated inventory refusal", runner.mutations)
		}
	})
}

func TestInspectExactLocalOperatorGroupRejectsMissingMalformedOrTruncatedEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*scriptedGroupRunner){
		"missing record name": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nPrimaryGroupID: 701\n")
		},
		"missing primary gid": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nRecordName: boxwarden-operators\n")
		},
		"negative direct primary gid": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nPrimaryGroupID: -2\nRecordName: boxwarden-operators\n")
		},
		"missing GroupMembers": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembership: wes\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"missing GroupMembership": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"missing caller GUID": func(r *scriptedGroupRunner) {
			r.set(userReadArgs(), "RecordName: wes\nUniqueID: 501\n")
		},
		"malformed output": func(r *scriptedGroupRunner) { r.set(groupReadArgs(), "not-an-attribute\n") },
		"duplicate attribute": func(r *scriptedGroupRunner) {
			r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nPrimaryGroupID: 701\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		},
		"truncated output": func(r *scriptedGroupRunner) { r.truncated[scriptedKey(groupReadArgs())] = true },
		"command failure":  func(r *scriptedGroupRunner) { r.failures[scriptedKey(groupReadArgs())] = errors.New("failure") },
	} {
		t.Run(name, func(t *testing.T) {
			runner := exactGroupSnapshotRunner("")
			mutate(runner)
			if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, false); err == nil {
				t.Fatal("inspectExactLocalOperatorGroup() error = nil, want incomplete evidence refusal")
			}
		})
	}
}

func TestInspectExactLocalOperatorGroupAllowsOnlyCompletelyEmptyPremutationMembership(t *testing.T) {
	runner := exactGroupSnapshotRunner("")
	runner.set(groupReadArgs(), "PrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
	group, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, true)
	if err != nil || !reflect.DeepEqual(group, Group{ID: 701, Name: OperatorGroupName}) {
		t.Fatalf("empty pre-mutation snapshot = %#v, %v", group, err)
	}
	runner.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
	if _, err := inspectExactLocalOperatorGroup(runner, Operator{UID: 501, Name: "wes", Home: "/Users/wes"}, OperatorGroupName, true); err == nil {
		t.Fatal("partially populated pre-mutation group was accepted")
	}
}

func TestDarwinGroupManagerUsesExactSnapshotBeforeMutationAndRevalidatesAfter(t *testing.T) {
	caller := Caller{UID: 501, Name: "wes", Home: "/Users/wes"}
	lookup := func(string) (*user.Group, error) { return &user.Group{Gid: "701", Name: OperatorGroupName}, nil }

	unsafe := exactGroupSnapshotRunner("")
	unsafe.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nNestedGroups: NESTED-GUID\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
	if _, _, err := (darwinGroupManager{runner: unsafe, lookupGroup: lookup}).Ensure(caller, OperatorGroupName); err == nil {
		t.Fatal("Ensure() error = nil, want pre-mutation nested-group refusal")
	}
	if unsafe.mutations != 0 {
		t.Fatalf("directory mutations = %d, want zero before exhaustive snapshot", unsafe.mutations)
	}

	empty := exactGroupSnapshotRunner("")
	empty.set(groupReadArgs(), "PrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
	empty.set(userReadArgs(), "GeneratedUID: CALLER-GUID\nRecordName: wes com.apple.idms.appleid.prd.example-local-alias\nUniqueID: 501\n")
	empty.promoteOnEdit = true
	group, changed, err := (darwinGroupManager{runner: empty, lookupGroup: lookup}).Ensure(caller, OperatorGroupName)
	if err != nil || !changed || !reflect.DeepEqual(group, Group{ID: 701, Name: OperatorGroupName, Members: []int{501}}) {
		t.Fatalf("Ensure(empty) = %#v, %t, %v; want exact revalidated caller", group, changed, err)
	}
	if empty.mutations != 1 {
		t.Fatalf("directory mutations = %d, want one exact member edit", empty.mutations)
	}
}

type scriptedGroupRunner struct {
	results       map[string]execx.Result
	failures      map[string]error
	truncated     map[string]bool
	commands      []execx.Command
	mutations     int
	promoteOnEdit bool
}

func exactGroupSnapshotRunner(primaryUsers string) *scriptedGroupRunner {
	runner := &scriptedGroupRunner{results: map[string]execx.Result{}, failures: map[string]error{}, truncated: map[string]bool{}}
	if primaryUsers == "" {
		primaryUsers = "wes 20\n"
	}
	runner.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
	runner.set(userReadArgs(), "GeneratedUID: CALLER-GUID\nRecordName: wes\nUniqueID: 501\n")
	runner.set(primaryUserListArgs(), primaryUsers)
	runner.set(groupGIDListArgs(), "boxwarden-operators 701\nstaff 20\n")
	return runner
}

func groupReadArgs() []string {
	return []string{"/Local/Default", "-read", "/Groups/" + OperatorGroupName}
}
func userReadArgs() []string {
	return []string{"/Local/Default", "-read", "/Users/wes", "RecordName", "UniqueID", "GeneratedUID"}
}
func primaryUserListArgs() []string {
	return []string{"/Search", "-list", "/Users", "PrimaryGroupID"}
}
func groupGIDListArgs() []string {
	return []string{"/Search", "-list", "/Groups", "PrimaryGroupID"}
}
func scriptedKey(args []string) string { return strings.Join(args, "\x00") }
func (r *scriptedGroupRunner) set(args []string, stdout string) {
	r.results[scriptedKey(args)] = execx.Result{Stdout: stdout}
}
func (r *scriptedGroupRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	r.commands = append(r.commands, command)
	if command.Path == "/usr/sbin/dseditgroup" {
		want := []string{"-o", "edit", "-n", "/Local/Default", "-a", "wes", "-t", "user", OperatorGroupName}
		if !r.promoteOnEdit || !reflect.DeepEqual(command.Args, want) || !reflect.DeepEqual(command.Env, []string{"LC_ALL=C", "LANG=C"}) {
			return execx.Result{}, fmt.Errorf("unexpected directory mutation: %#v", command)
		}
		r.mutations++
		r.set(groupReadArgs(), "GroupMembers: CALLER-GUID\nGroupMembership: wes\nPrimaryGroupID: 701\nRecordName: boxwarden-operators\n")
		return execx.Result{}, nil
	}
	key := scriptedKey(command.Args)
	result, ok := r.results[key]
	if !ok {
		return execx.Result{}, fmt.Errorf("unexpected command: %v", command.Args)
	}
	result.Truncated = r.truncated[key]
	return result, r.failures[key]
}
