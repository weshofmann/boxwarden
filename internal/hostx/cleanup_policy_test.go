package hostx

import (
	"errors"
	"github.com/weshofmann/boxwarden/internal/execx"
	"os"
	"reflect"
	"testing"
)

func TestCleanupPolicyArgumentsAndNonzeroRefusalPreserveOrdinaryDenial(t *testing.T) {
	for _, x := range []struct {
		op   string
		uid  int
		want []string
	}{{"", 501, []string{"-n", "-ll"}}, {"devel", 0, []string{"-U", "devel", "-ll"}}, {"devel", 501, nil}, {"other", 0, nil}} {
		args, e := sudoPolicyArguments(x.op, x.uid)
		if x.want == nil {
			if e == nil {
				t.Fatal("unadmitted root-policy query")
			}
		} else if e != nil || !reflect.DeepEqual(args, x.want) {
			t.Fatal("argv count/bytes", args, e)
		}
	}
	for _, text := range []string{"not allowed to run sudo", "not in the sudoers file", "may not run sudo"} {
		r := execx.Result{Stderr: text, StderrComplete: true}
		if _, e := sudoPolicyResult(r, errors.New("exit1"), true, "/opt/homebrew/bin/softnet"); e == nil {
			t.Fatal("root nonzero denial assumed safe")
		}
		ok, e := sudoPolicyResult(r, errors.New("exit1"), false, "/opt/homebrew/bin/softnet")
		if e != nil || ok {
			t.Fatal("ordinary denial changed", ok, e)
		}
	}
	r := execx.Result{Stdout: "Sudoers entry:\n    RunAsUsers: root\n    Options: !authenticate\n    Commands:\n        /opt/homebrew/bin/softnet\n", StderrComplete: true}
	if ok, e := sudoPolicyResult(r, nil, true, "/opt/homebrew/bin/softnet"); e != nil || !ok {
		t.Fatal("root parser exact mutable target", ok, e)
	}
	r.Truncated = true
	if _, e := sudoPolicyResult(r, nil, true, "/opt/homebrew/bin/softnet"); e == nil {
		t.Fatal("truncated root query")
	}
	r.Truncated = false
	r.StderrComplete = false
	if _, e := sudoPolicyResult(r, nil, true, "/opt/homebrew/bin/softnet"); e == nil {
		t.Fatal("unknown drain")
	}
	if os.Geteuid() != 0 {
		i := &osDoctorInspector{policyOperator: "devel", runner: policyRunnerFake{}}
		if _, e := i.passwordlessRoot("/opt/homebrew/bin/softnet"); e == nil {
			t.Fatal("nonroot query admitted")
		}
	}
}
func TestCleanupDirectoryMembershipDoesNotFabricateRootEffectiveGroups(t *testing.T) {
	i, r := healthyDoctorFixture(t)
	i.effectiveGroups = nil
	s := SystemDoctor{inspector: i}
	if s.inspect(t.Context(), r).report.Status == Healthy {
		t.Fatal("ordinary policy weakened")
	}
	if s.inspectPolicy(t.Context(), r, false).report.Status != Healthy {
		t.Fatal("read-only directory admission incorrectly demanded root effective groups")
	}
	i.group.Members = append(i.group.Members, 999)
	if s.inspectPolicy(t.Context(), r, false).report.Status == Healthy {
		t.Fatal("directory membership drift admitted")
	}
}
