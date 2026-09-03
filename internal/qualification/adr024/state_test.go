package adr024

import (
	"strings"
	"testing"
)

func TestExpectedFromManifestUsesStrictFixedHostIdentity(t *testing.T) {
	raw := validManifestJSON()
	expected, err := expectedFromManifest(raw, 410, 12_001, 12_001, 20, 20, []int{20, 12_002})
	if err != nil {
		t.Fatalf("expectedFromManifest() error = %v", err)
	}
	if expected != (Expected{
		TartPID:           410,
		TartExecutable:    "/opt/homebrew/Cellar/tart/2.32.1/libexec/tart.app/Contents/MacOS/tart",
		SoftnetExecutable: "/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet",
		OperatorUID:       12_001,
		OperatorGID:       20,
	}) {
		t.Fatalf("expected = %#v", expected)
	}

	duplicate := strings.Replace(string(raw), `"version":2`, `"version":2,"version":2`, 1)
	if _, err := expectedFromManifest([]byte(duplicate), 410, 12_001, 12_001, 20, 20, []int{20, 12_002}); err == nil {
		t.Fatal("duplicate manifest field accepted")
	}
	if _, err := expectedFromManifest(raw, 410, 12_003, 12_003, 20, 20, []int{20, 12_002}); err == nil {
		t.Fatal("non-manifested operator accepted")
	}
	if _, err := expectedFromManifest(raw, 410, 12_001, 12_001, 20, 20, []int{20}); err == nil {
		t.Fatal("operator without effective qualified group accepted")
	}
}

func validManifestJSON() []byte {
	return []byte(`{"version":2,"platform":"darwin","macos":"26.6.2","macos_build":"25G83","tart":{"path":"/opt/homebrew/Cellar/tart/2.32.1/libexec/tart.app/Contents/MacOS/tart","version":"2.32.1","executable_sha256":"05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d","archive_sha256":"8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529"},"softnet":{"path":"/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet","version":"0.19.0","executable_sha256":"ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e","archive_sha256":"1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"},"root_uid":0,"group":{"id":12002,"name":"boxwarden-operators","members":[12001]},"operator":{"uid":12001,"name":"operator","home":"/Users/operator"},"tart_home":"/private/tmp/boxwarden-test-tart-home","softnet_mode":2408,"installed_at":"2026-09-03T00:57:19.046851Z"}`)
}
