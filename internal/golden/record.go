// Package golden records explicitly registered, stopped backend goldens.
package golden

import (
	"fmt"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const recordVersion = 1

// BackendRef identifies the immutable backend object that provides a golden.
type BackendRef struct {
	Kind     string `json:"kind"`
	ObjectID string `json:"object_id"`
}

// Record is a versioned, domain-owned immutable golden registration.
type Record struct {
	Version  int        `json:"version"`
	Domain   domain.ID  `json:"domain"`
	Revision string     `json:"revision"`
	Backend  BackendRef `json:"backend"`
}

func validTartName(raw string) bool {
	if len(raw) == 0 || len(raw) > 127 || !isAlphaNumeric(raw[0]) {
		return false
	}
	for index := 1; index < len(raw); index++ {
		if !isAlphaNumeric(raw[index]) && raw[index] != '-' {
			return false
		}
	}
	return true
}

func validRecord(record Record) error {
	if record.Version != recordVersion {
		return fmt.Errorf("unsupported golden record version %d", record.Version)
	}
	if _, err := domain.Parse(string(record.Domain)); err != nil {
		return fmt.Errorf("invalid golden record domain: %w", err)
	}
	if !validTartName(record.Revision) || record.Backend.Kind != "tart" || record.Backend.ObjectID != record.Revision {
		return fmt.Errorf("invalid golden backend reference")
	}
	return nil
}

func isAlphaNumeric(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}
