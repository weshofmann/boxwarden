package session

import "testing"

func TestParseNameAcceptsSafeLowercaseASCIIName(t *testing.T) {
	name, err := ParseName("dev1")
	if err != nil {
		t.Fatalf("ParseName(dev1) error = %v", err)
	}
	if got, want := string(name), "dev1"; got != want {
		t.Fatalf("ParseName(dev1) = %q, want %q", got, want)
	}
}

func TestParseNameRejectsPathAndControlInputs(t *testing.T) {
	for _, raw := range []string{"", ".", "..", "../work", "/work", "--delete", "Work", "work-name", "work name", "work\x00", "w​ork", "a123456789012345678901234567890123456789012345678901234567890123"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseName(raw); err == nil {
				t.Fatalf("ParseName(%q) error = nil, want invalid name error", raw)
			}
		})
	}
}
