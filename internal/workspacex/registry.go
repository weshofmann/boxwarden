package workspacex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	maxRegistryEntries    = 4096
	maxSessionAttachments = 4
)

// ListSessionAttachments reads the volume-owned attachment authority under
// the domain storage lock. A corrupt registry blocks launch admission even
// when the malformed record appears to belong to another session.
func ListSessionAttachments(ctx context.Context, stateRoot string, domainID domain.ID, sessionID, sessionName string) ([]Record, error) {
	if !validUUID(sessionID) || !validSessionName(sessionName) {
		return nil, fmt.Errorf("invalid session attachment binding")
	}
	held, err := AcquireStorageOperation(ctx, stateRoot, domainID)
	if err != nil {
		return nil, err
	}
	defer held.Release()
	return listSessionAttachments(ctx, stateRoot, domainID, sessionID, sessionName)
}

// listSessionAttachments is also usable by a lifecycle transition that already
// holds the domain storage lock after its volume-use and session locks.
func listSessionAttachments(ctx context.Context, stateRoot string, domainID domain.ID, sessionID, sessionName string) ([]Record, error) {
	if !validUUID(sessionID) || !validSessionName(sessionName) {
		return nil, fmt.Errorf("invalid session attachment binding")
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	workspaces, err := openChild(root, "workspaces", false)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer workspaces.Close()
	directory, err := workspaces.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(maxRegistryEntries + 1)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if len(entries) > maxRegistryEntries {
		return nil, fmt.Errorf("workspace registry exceeds %d entries", maxRegistryEntries)
	}
	selected := make([]Record, 0)
	seenFilesystem := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		volumeID := name[:len(name)-len(".json")]
		if !validUUID(volumeID) {
			return nil, fmt.Errorf("invalid workspace record name %q", name)
		}
		record, err := loadFromRoot(workspaces, domainID, volumeID)
		if err != nil {
			return nil, fmt.Errorf("read workspace %q: %w", volumeID, err)
		}
		if record.Attachment == nil {
			continue
		}
		binding := record.Attachment
		idMatches, nameMatches := binding.SessionID == sessionID, binding.SessionName == sessionName
		if idMatches != nameMatches {
			return nil, fmt.Errorf("workspace attachment has conflicting session identity")
		}
		if binding.SessionID != sessionID {
			continue
		}
		if seenFilesystem[record.FilesystemUUID] {
			return nil, fmt.Errorf("session has duplicate workspace filesystem UUID")
		}
		for _, prior := range selected {
			if mountsOverlap(prior.Attachment.MountPath, binding.MountPath) {
				return nil, fmt.Errorf("session has overlapping workspace mounts")
			}
		}
		seenFilesystem[record.FilesystemUUID] = true
		selected = append(selected, record)
		if len(selected) > maxSessionAttachments {
			return nil, fmt.Errorf("session exceeds %d workspace attachments", maxSessionAttachments)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].VolumeID < selected[j].VolumeID })
	return selected, nil
}
