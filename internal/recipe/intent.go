package recipe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// CanonicalIntent identifies the complete validated recipe independently of
// JSON whitespace and the reusable base's narrower preparation key. The
// returned bytes are the immutable session-intent snapshot to persist before
// a session references its digest.
func CanonicalIntent(value Recipe) ([]byte, string, error) {
	if err := value.validate(); err != nil {
		return nil, "", err
	}
	// Missing and empty lists have identical recipe meaning.
	value.AptPackages = append([]string{}, value.AptPackages...)
	value.Steps = append([]Step{}, value.Steps...)
	value.Workspaces = append([]Workspace{}, value.Workspaces...)
	value.Launch = append([]Launch{}, value.Launch...)
	encoded, err := json.Marshal(struct {
		IntentVersion int    `json:"intent_version"`
		Recipe        Recipe `json:"recipe"`
	}{IntentVersion: 1, Recipe: value})
	if err != nil {
		return nil, "", fmt.Errorf("encode recipe intent: %w", err)
	}
	if len(encoded) > maxRecipeBytes {
		return nil, "", fmt.Errorf("canonical recipe intent exceeds %d bytes", maxRecipeBytes)
	}
	digest := sha256.Sum256(encoded)
	return encoded, fmt.Sprintf("%x", digest[:]), nil
}
