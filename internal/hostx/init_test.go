package hostx

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestSystemInitValidatesUnprivilegedStateBeforeExactPrivilegeTransition(t *testing.T) {
	inspector, request := healthyDoctorFixture(t)
	runner := &privilegeRunnerFake{result: execx.Result{Stdout: `{"published":true,"already_installed":false,"refresh_login_session":true}`}}
	validated := ""
	service := SystemService{
		inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden",
		sourceValidator: func(path, digest string) error { validated = path + ":" + digest; return nil },
	}
	result, err := service.Init(context.Background(), request)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !result.HostInstalled || result.DomainInitialized || !result.RefreshLoginSession {
		t.Fatalf("Init() = %#v, want host-only publication boundary", result)
	}
	if validated != request.SoftnetPath+":"+SoftnetExecutableSHA256 || runner.command.Path != "/usr/bin/sudo" {
		t.Fatalf("preflight/transition = %q %#v", validated, runner.command)
	}
}

func TestSystemInitRefusesUnsafeHomebrewOrUnsupportedPlatformWithoutPrivilege(t *testing.T) {
	for name, mutate := range map[string]func(*doctorInspectorFake){
		"unsafe Homebrew": func(inspector *doctorInspectorFake) {
			inspector.homebrew = []HomebrewSoftnet{{Path: "/opt/homebrew/bin/softnet", Privilege: "setuid"}}
		},
		"unsupported platform": func(inspector *doctorInspectorFake) { inspector.platform.OS = "linux" },
	} {
		t.Run(name, func(t *testing.T) {
			inspector, request := healthyDoctorFixture(t)
			mutate(inspector)
			runner := &privilegeRunnerFake{}
			service := SystemService{inspector: inspector, privilege: runner, executable: "/opt/boxwarden/bin/boxwarden", sourceValidator: func(string, string) error { return nil }}
			if _, err := service.Init(context.Background(), request); err == nil {
				t.Fatal("Init() error = nil, want fail-closed refusal")
			}
			if runner.command.Path != "" {
				t.Fatalf("privilege runner called after failed preflight: %#v", runner.command)
			}
		})
	}
}
