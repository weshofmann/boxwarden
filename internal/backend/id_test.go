package backend

import (
	"strings"
	"testing"
)

func TestValidateObjectIDAcceptsSafeASCIIIdentifier(t *testing.T) {
	for _, objectID := range []string{"a", "boxwarden-work-dev", "A9-z"} {
		if err := ValidateObjectID(objectID); err != nil {
			t.Fatalf("ValidateObjectID(%q) error = %v, want nil", objectID, err)
		}
	}
}

func TestValidateObjectIDRejectsUnsafeOrOverlongIdentifier(t *testing.T) {
	overlong := "a" + strings.Repeat("b", 127)
	for _, objectID := range []string{
		"", "-option", "--delete", "/path", "path/object", "object\\path",
		"object name", "object\x00", "object\nname", "object_name", "object.1",
		"\u0441onfusable", overlong,
	} {
		t.Run(objectID, func(t *testing.T) {
			if err := ValidateObjectID(objectID); err == nil {
				t.Fatalf("ValidateObjectID(%q) error = nil, want rejection", objectID)
			}
		})
	}
}
