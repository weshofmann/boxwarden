package recipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
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

// DecodeIntent re-admits an exact stored snapshot before its action fields are
// used. Re-encoding rejects alternate JSON spellings under the same semantic
// value; the private store separately checks its content-addressed digest.
func DecodeIntent(raw []byte) (Recipe, error) {
	if len(raw) == 0 || len(raw) > maxRecipeBytes {
		return Recipe{}, fmt.Errorf("recipe intent length is invalid")
	}
	if err := rejectDuplicateKeys(raw); err != nil {
		return Recipe{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope struct {
		IntentVersion int    `json:"intent_version"`
		Recipe        Recipe `json:"recipe"`
	}
	if err := decoder.Decode(&envelope); err != nil {
		return Recipe{}, fmt.Errorf("decode recipe intent: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return Recipe{}, fmt.Errorf("recipe intent has trailing data")
	}
	if envelope.IntentVersion != 1 {
		return Recipe{}, fmt.Errorf("unsupported recipe intent version")
	}
	canonical, _, err := CanonicalIntent(envelope.Recipe)
	if err != nil {
		return Recipe{}, err
	}
	if !bytes.Equal(raw, canonical) {
		return Recipe{}, fmt.Errorf("recipe intent is not canonical")
	}
	return envelope.Recipe, nil
}
