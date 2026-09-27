package sshx

import (
	"fmt"
	"os"
	"path/filepath"
)

// CleanupGenerationCredentials removes only the four fixed generation-local
// management files after the retained VM handle has reaped. It pre-admits all
// present entries before removal and refuses symlinks, ACLs and replacements.
func CleanupGenerationCredentials(runtimeDirectory string) error {
	if err := requirePrivateTree(runtimeDirectory, runtimeDirectory); err != nil {
		return fmt.Errorf("private generation: %w", err)
	}
	type candidate struct {
		path string
		mode os.FileMode
		info os.FileInfo
	}
	var files []candidate
	for _, entry := range []struct {
		name string
		mode os.FileMode
	}{{"known_hosts", privateFileMode}, {"client-cert.pub", publicFileMode}, {"client.pub", publicFileMode}, {"client", privateFileMode}} {
		path := filepath.Join(runtimeDirectory, entry.name)
		info, err := requireRuntimeFile(runtimeDirectory, path, entry.mode)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("admit generation credential %q: %w", entry.name, err)
		}
		files = append(files, candidate{path: path, mode: entry.mode, info: info})
	}
	for _, file := range files {
		if err := removeExactFile(file.path, file.info, file.mode); err != nil {
			return fmt.Errorf("remove generation credential %q: %w", filepath.Base(file.path), err)
		}
	}
	return nil
}
