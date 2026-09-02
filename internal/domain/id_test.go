package domain

import "testing"

func TestParseAcceptsLowercaseASCIIIdentifier(t *testing.T) {
	id, err := Parse("work")
	if err != nil {
		t.Fatalf("Parse(work) error = %v", err)
	}
	if got, want := string(id), "work"; got != want {
		t.Fatalf("Parse(work) = %q, want %q", got, want)
	}
}

func TestParseRejectsUnsafeDomainIdentifiers(t *testing.T) {
	for _, raw := range []string{"", "Work", "work-domain", "../work", "work/other", "work\x00", "work space", "work_", "a123456789012345678901234567890123456789012345678901234567890123"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("Parse(%q) error = nil, want invalid identifier error", raw)
			}
		})
	}
}
