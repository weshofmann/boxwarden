package recipe

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// PreparationKey identifies only inputs that can change a reusable base.
// Session-specific workspaces, once/reconfigure/startup steps, and launch
// intent run after cloning and therefore do not invalidate the shared base.
// The guest definition digest must cover every tracked build input, including
// the locked bootstrap artifact; callers must validate that snapshot before
// starting a build.
func PreparationKey(value Recipe, guestDefinitionSHA256 string) (string, error) {
	if err := value.validate(); err != nil {
		return "", err
	}
	if len(guestDefinitionSHA256) != 64 {
		return "", fmt.Errorf("guest definition SHA-256 must be 64 lowercase hex characters")
	}
	for _, character := range guestDefinitionSHA256 {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return "", fmt.Errorf("guest definition SHA-256 must be lowercase hex")
		}
	}
	preparation := struct {
		KeyVersion            int      `json:"key_version"`
		GuestDefinitionSHA256 string   `json:"guest_definition_sha256"`
		Source                Source   `json:"source"`
		Machine               Machine  `json:"machine"`
		AptPackages           []string `json:"apt_packages"`
		Steps                 []Step   `json:"prepare_steps"`
	}{KeyVersion: 1, GuestDefinitionSHA256: guestDefinitionSHA256, Source: value.Source,
		Machine: value.Machine, AptPackages: value.AptPackages}
	for _, step := range value.Steps {
		if step.Phase == "prepare" {
			preparation.Steps = append(preparation.Steps, step)
		}
	}
	encoded, err := json.Marshal(preparation)
	if err != nil {
		return "", fmt.Errorf("encode preparation inputs: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}
