package projectx

import (
	"strings"
	"testing"
)

func TestPrebuiltSetupRequiresResourcesAndNoGoCompiler(t *testing.T) {
	s := fixtureSetup()
	s.Version = 3
	s.GoBinary = ""
	s.OpenSSLPath = "/tools/openssl"
	s.XorrisoPath = "/tools/xorriso"
	s.OpenSSLSHA256 = strings.Repeat("a", 64)
	s.XorrisoSHA256 = strings.Repeat("b", 64)
	raw := mustJSON(t, s)
	raw = append(raw[:len(raw)-1], []byte(`,"prebuilt_resources":"/package/prebuilt"}`)...)
	var got Setup
	if err := decodeSetup(raw, &got); err != nil {
		t.Fatalf("version3 prebuilt setup rejected: %v", err)
	}
	if err := validateSetup(got); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{`"/package/prebuilt"`, `"/tools/openssl"`} {
		var bad Setup
		changed := strings.Replace(string(raw), mutation, `""`, 1)
		if err := decodeSetup([]byte(changed), &bad); err == nil && validateSetup(bad) == nil {
			t.Fatalf("missing required input admitted: %s", mutation)
		}
	}
}
