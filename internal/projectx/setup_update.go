package projectx

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// UpdateSetup explicitly replaces only the remembered asset locators. The
// caller holds the setup lock and admits next through the existing checker.
// Exact previous bytes are durably archived before active publication. Runtime
// workspace, import and export records are never rewritten by this operation.
func UpdateSetup(stateRoot string, next Setup) (string, error) {
	if err := validateSetup(next); err != nil {
		return "", err
	}
	dir, err := openProjects(stateRoot, false)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	raw, info, err := readDocument(dir, setupName)
	if err != nil {
		return "", err
	}
	var prior Setup
	if err := decodeDocument(raw, &prior, setupFields); err != nil {
		return "", err
	}
	if err := validateSetup(prior); err != nil {
		return "", err
	}
	if next == prior {
		return "", syncProjectDirectory(dir)
	}
	history := fmt.Sprintf(".setup-history-%x.json", sha256.Sum256(raw))
	existing, _, err := readDocument(dir, history)
	if err == nil {
		if !bytes.Equal(existing, raw) {
			return "", errors.New("setup history differs from exact previous profile; active setup was not changed")
		}
		// A previous attempt may have published its archive and failed directory
		// sync. Finish that sync before changing the active setup on a retry.
		if err := syncProjectDirectory(dir); err != nil {
			return "", err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := publishRaw(dir, history, raw, nil); err != nil {
			return "", fmt.Errorf("preserve previous setup: %w", err)
		}
	} else {
		return "", err
	}
	archived := filepath.Join(dir.Name(), history)
	if err := publish(dir, setupName, next, info); err != nil {
		return archived, fmt.Errorf("publish updated setup (it may already be active): %w", err)
	}
	return archived, nil
}
