package backend

import "fmt"

const maxObjectIDLength = 127

// ValidateObjectID admits backend object identifiers that are safe to pass as
// a single argv operand. Backend-specific policy belongs in the adapter.
func ValidateObjectID(objectID string) error {
	if len(objectID) == 0 || len(objectID) > maxObjectIDLength || !isAlphaNumeric(objectID[0]) {
		return fmt.Errorf("invalid backend object ID %q", objectID)
	}
	for index := 1; index < len(objectID); index++ {
		if !isAlphaNumeric(objectID[index]) && objectID[index] != '-' {
			return fmt.Errorf("invalid backend object ID %q", objectID)
		}
	}
	return nil
}

func isAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}
