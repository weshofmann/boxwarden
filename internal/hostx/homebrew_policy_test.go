package hostx

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestDetectPasswordlessRuleBindsVerboseSudoStanzaToExactMutableHomebrewSoftnet(t *testing.T) {
	target := "/opt/homebrew/bin/softnet"
	for name, test := range map[string]struct {
		output string
		want   bool
	}{
		"passworded all and unrelated passwordless command": {`Matching Defaults entries for wes on host:
    env_reset

User wes may run the following commands on host:
    (root) ALL
    (root) NOPASSWD: /usr/bin/pmset

Sudoers entry:
    RunAsUsers: root
    Commands:
        ALL

Sudoers entry:
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /usr/bin/pmset
`, false},
		"source-suffixed header with exact command and arguments": {`Sudoers entry: /private/etc/sudoers.d/90-softnet
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet --allow 10.0.0.0/8
`, true},
		"source-suffixed header with all commands": {`Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Options: !authenticate
    Commands:
        ALL
`, true},
		"command before authentication option is malformed": {`Sudoers entry:
    RunAsUsers: root
    Commands:
        /opt/homebrew/bin/softnet
    Options: !authenticate
`, false},
		"authentication option without commands is malformed": {`Sudoers entry:
    RunAsUsers: root
    Options: !authenticate
`, false},
		"ambiguous command continuation is rejected": {`Sudoers entry:
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet,
        not-a-command
`, false},
		"lookalike stanza header is rejected": {`Sudoers entry source: /private/etc/sudoers.d/90-softnet
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet
`, false},
		"source suffix without separator is rejected": {`Sudoers entry:/private/etc/sudoers.d/90-softnet
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet
`, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := detectPasswordlessRule(test.output, target); got != test.want {
				t.Fatalf("detectPasswordlessRule() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPasswordlessPolicyInspectionRejectsAmbiguousMatchingDefaults(t *testing.T) {
	for name, output := range map[string]string{
		"command-specific defaults disable authentication": `Runas and Command-specific defaults for wes on host:
    Defaults!/opt/homebrew/bin/softnet !authenticate

Sudoers entry: /private/etc/sudoers.d/90-softnet
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet
`,
		"command-specific defaults leave exempt group ambiguous": `Matching Defaults entries for wes on host:
    exempt_group

Sudoers entry: /private/etc/sudoers.d/90-softnet
    RunAsUsers: root
    Commands:
        /usr/bin/id
`,
	} {
		t.Run(name, func(t *testing.T) {
			inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stdout: output}}}
			if _, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet"); err == nil {
				t.Fatal("passwordlessRoot() error = nil, want unsupported Defaults policy rejection")
			}
		})
	}
}

func TestPasswordlessPolicyInspectionAppliesMatchingDefaultsToEachStanza(t *testing.T) {
	for name, test := range map[string]struct {
		output string
		target string
		want   bool
	}{
		"inherited no-auth exact target": {`Matching Defaults entries for wes on host:
    env_reset, !authenticate

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Commands:
        /opt/homebrew/bin/softnet
`, "/opt/homebrew/bin/softnet", true},
		"inherited no-auth unrelated target": {`Matching Defaults entries for wes on host:
    env_reset, !authenticate

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Commands:
        /usr/bin/id
`, "/opt/homebrew/bin/softnet", false},
		"unrelated stanza option preserves inherited no-auth": {`Matching Defaults entries for wes on host:
    !authenticate

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Options: setenv
    Commands:
        /opt/homebrew/bin/softnet
`, "/opt/homebrew/bin/softnet", true},
		"quoted option text does not override inherited no-auth": {`Matching Defaults entries for wes on host:
    !authenticate, env_keep="authenticate"

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Commands:
        /opt/homebrew/bin/softnet
`, "/opt/homebrew/bin/softnet", true},
		"multi-word quoted option does not synthesize authenticate": {`Matching Defaults entries for wes on host:
    !authenticate, env_keep+="FOO authenticate BAR"

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Commands:
        /opt/homebrew/bin/softnet
`, "/opt/homebrew/bin/softnet", true},
		"stanza authenticate overrides inherited no-auth": {`Matching Defaults entries for wes on host:
    !authenticate

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Options: authenticate
    Commands:
        /opt/homebrew/bin/softnet
`, "/opt/homebrew/bin/softnet", false},
	} {
		t.Run(name, func(t *testing.T) {
			inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stdout: test.output}}}
			got, err := inspector.passwordlessRoot(test.target)
			if err != nil {
				t.Fatalf("passwordlessRoot() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("passwordlessRoot() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPasswordlessPolicyInspectionFailsClosedForMalformedSudoOptions(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stdout: `Matching Defaults entries for wes on host:
    !authenticate, env_keep+="FOO

Sudoers entry: /private/etc/sudoers
    RunAsUsers: root
    Commands:
        /opt/homebrew/bin/softnet
`}}}
	if _, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet"); err == nil {
		t.Fatal("passwordlessRoot() error = nil, want malformed option rejection")
	}
}

func TestVerboseSudoCommandMatchingFailsClosedForUnsupportedSyntax(t *testing.T) {
	target := "/opt/homebrew/bin/softnet"
	for name, test := range map[string]struct {
		specification string
		want          bool
		wantErr       bool
	}{
		"exact target with ordinary arguments": {"/opt/homebrew/bin/softnet --allow 10.0.0.0/8", true, false},
		"exact all":                            {"ALL", true, false},
		"direct child of exact directory":      {"/opt/homebrew/bin/", true, false},
		"nested child of exact directory":      {"/opt/homebrew/", false, false},
		"wildcarded prefix":                    {"/opt/homebrew/bin/*", false, true},
		"posix negated character class":        {"/opt/homebrew/bin/[!x]oftnet", false, true},
		"unrelated plain absolute command":     {"/usr/bin/id", false, false},
		"digest command specification":         {"sha256:aaaaaaaa /opt/homebrew/bin/softnet", false, true},
		"negated command specification":        {"!/opt/homebrew/bin/softnet", false, true},
		"escaped command specification":        {`/opt/homebrew/bin/softnet\ `, false, true},
		"malformed quoted argument":            {`/opt/homebrew/bin/softnet "unterminated`, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := sudoCommandMatchesTarget(test.specification, target)
			if (err != nil) != test.wantErr {
				t.Fatalf("sudoCommandMatchesTarget() error = %v, want error %t", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("sudoCommandMatchesTarget() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestPasswordlessPolicyInspectionFailsClosedForMalformedVerboseSudoList(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stdout: `Sudoers entry:
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet,
        not-a-command
`}}}
	if _, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet"); err == nil {
		t.Fatal("passwordlessRoot(malformed verbose sudo list) error = nil, want fail-closed result")
	}
}

func TestPasswordlessPolicyInspectionUsesNoninteractiveVerboseFullSudoList(t *testing.T) {
	runner := &recordingPolicyRunner{result: execx.Result{Stdout: `Sudoers entry:
    RunAsUsers: root
    Options: !authenticate
    Commands:
        /opt/homebrew/bin/softnet *
`}}
	inspector := &osDoctorInspector{runner: runner}

	got, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet")
	if err != nil {
		t.Fatalf("passwordlessRoot() error = %v", err)
	}
	if !got {
		t.Fatal("passwordlessRoot() = false, want true")
	}
	want := execx.Command{
		Path: "/usr/bin/sudo",
		Args: []string{"-n", "-ll"},
		Env:  []string{"LC_ALL=C", "LANG=C", "PATH=/usr/bin:/bin:/usr/sbin:/sbin"},
	}
	if !reflect.DeepEqual(runner.command, want) {
		t.Fatalf("sudo command = %#v, want %#v", runner.command, want)
	}
}

func TestPasswordlessPolicyInspectionFailsClosedWhenSudoCannotProvePolicy(t *testing.T) {
	inspector := &osDoctorInspector{runner: policyRunnerFake{result: execx.Result{Stderr: "sudo: a password is required\n"}, err: errors.New("exit status 1")}}
	if _, err := inspector.passwordlessRoot("/opt/homebrew/bin/softnet"); err == nil {
		t.Fatal("passwordlessRoot(unavailable policy) error = nil, want fail-closed result")
	}
}

type policyRunnerFake struct {
	result execx.Result
	err    error
}

func (f policyRunnerFake) Run(context.Context, execx.Command) (execx.Result, error) {
	return f.result, f.err
}

type recordingPolicyRunner struct {
	result  execx.Result
	err     error
	command execx.Command
}

func (f *recordingPolicyRunner) Run(_ context.Context, command execx.Command) (execx.Result, error) {
	f.command = command
	return f.result, f.err
}
