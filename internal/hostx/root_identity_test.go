package hostx

import (
	"os/user"
	"testing"
)

func TestOSRootIdentityDerivesAndCrossChecksSudoCaller(t *testing.T) {
	values := map[string]string{"SUDO_UID": "501", "SUDO_USER": "wes"}
	identity := osRootIdentity{
		geteuid: func() int { return 0 },
		getenv:  func(name string) string { return values[name] },
		lookupID: func(id string) (*user.User, error) {
			return &user.User{Uid: id, Username: "wes", HomeDir: "/Users/wes"}, nil
		},
	}
	caller, err := identity.SudoCaller()
	if err != nil || caller != (Caller{UID: 501, Name: "wes", Home: "/Users/wes"}) {
		t.Fatalf("SudoCaller() = %#v, %v", caller, err)
	}
	values["SUDO_USER"] = "spoofed"
	if _, err := identity.SudoCaller(); err == nil {
		t.Fatal("SudoCaller(spoofed name) error = nil")
	}
}
