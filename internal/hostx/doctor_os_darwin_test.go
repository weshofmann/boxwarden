//go:build darwin

package hostx

import (
	"context"
	"reflect"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestOSDoctorPlatformUsesAbsoluteVersionAndBuildProbes(t *testing.T) {
	runner := &platformProbeRunner{results: []execx.Result{{Stdout: QualifiedMacOS + "\n"}, {Stdout: QualifiedMacOSBuild + "\n"}}}
	fact := (&osDoctorInspector{runner: runner}).Platform()
	if fact.Release != QualifiedMacOS || fact.Build != QualifiedMacOSBuild {
		t.Fatalf("Platform() = %#v, want qualified release and build", fact)
	}
	want := []execx.Command{
		{Path: "/usr/bin/sw_vers", Args: []string{"-productVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}},
		{Path: "/usr/bin/sw_vers", Args: []string{"-buildVersion"}, Env: []string{"LC_ALL=C", "LANG=C"}},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("sw_vers commands = %#v, want %#v", runner.commands, want)
	}
}

type platformProbeRunner struct {
	results  []execx.Result
	commands []execx.Command
}

func (r *platformProbeRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	r.commands = append(r.commands, command)
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}
