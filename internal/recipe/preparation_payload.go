package recipe

import (
	"encoding/json"
	"errors"
	"fmt"
)

// PreparationPayload is the exact bounded guest-only intent placed in one
// disposable installer ISO. Session-specific steps never enter a shared base.
func PreparationPayload(value Recipe, key string) ([]byte, error) {
	if err := value.validate(); err != nil {
		return nil, err
	}
	if len(key) != 64 {
		return nil, errors.New("preparation key must be lowercase SHA-256")
	}
	for _, character := range key {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return nil, errors.New("preparation key must be lowercase SHA-256")
		}
	}
	type step struct {
		ID   string   `json:"id"`
		Argv []string `json:"argv"`
	}
	payload := struct {
		Version        int      `json:"version"`
		PreparationKey string   `json:"preparation_key"`
		AptPackages    []string `json:"apt_packages"`
		Steps          []step   `json:"steps"`
	}{Version: 1, PreparationKey: key, AptPackages: append([]string{}, value.AptPackages...), Steps: []step{}}
	for _, action := range value.Steps {
		if action.Phase == "prepare" {
			payload.Steps = append(payload.Steps, step{ID: action.ID, Argv: append([]string{}, action.Argv...)})
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode guest preparation payload: %w", err)
	}
	if len(raw) > maxRecipeBytes {
		return nil, errors.New("guest preparation payload exceeds bound")
	}
	return raw, nil
}
