package hostx

import (
	"strings"
	"testing"
)

func TestParseManifestRejectsUnknownFieldsAndWrongQualifiedIdentity(t *testing.T) {
	valid := `{"version":1,"platform":"darwin","macos":"26.6.2","tart":{"path":"/opt/qualified/tart","version":"2.32.1","executable_sha256":"05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d","archive_sha256":"8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529"},"softnet":{"path":"/Library/Boxwarden/toolchains/softnet/0.19.0/ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e/softnet","version":"0.19.0","executable_sha256":"ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e","archive_sha256":"1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"},"root_uid":0,"group":{"id":20,"name":"boxwarden-operators","members":[501]},"operator":{"uid":501,"name":"wes","home":"/Users/wes"},"tart_home":"/Users/wes/Library/Application Support/boxwarden/tart","softnet_mode":2408,"installed_at":"2026-09-01T00:00:00Z"}`

	for name, contents := range map[string]string{
		"unknown field":          strings.Replace(valid, `"version":1`, `"version":1,"extra":true`, 1),
		"duplicate field":        strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1),
		"missing field":          strings.Replace(valid, `"platform":"darwin",`, ``, 1),
		"wrong softnet digest":   strings.Replace(valid, SoftnetExecutableSHA256, strings.Repeat("0", 64), 1),
		"noncanonical tart path": strings.Replace(valid, `"/opt/qualified/tart"`, `"relative/tart"`, 1),
		"trailing JSON":          valid + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseManifest([]byte(contents)); err == nil {
				t.Fatal("ParseManifest() error = nil, want rejection")
			}
		})
	}

	manifest, err := ParseManifest([]byte(valid))
	if err != nil {
		t.Fatalf("ParseManifest() error = %v", err)
	}
	if manifest.Softnet.Path != QualifiedSoftnetPath {
		t.Fatalf("softnet path = %q, want %q", manifest.Softnet.Path, QualifiedSoftnetPath)
	}
}
