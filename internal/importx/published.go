package importx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
)

// ComparePublishedTree re-admits a captured source and compares it to one
// selected export of the guest's transaction directory. The caller must
// separately bind the export journal and stopped volume to the import.
func ComparePublishedTree(stagingParent, transactionID, publishedRoot string) (Snapshot, error) {
	snapshot, err := InspectSnapshot(stagingParent, transactionID)
	if err != nil {
		return Snapshot{}, err
	}
	if !canonicalAbsolute(publishedRoot) || publishedRoot == snapshot.Directory ||
		pathContains(publishedRoot, snapshot.Directory) || pathContains(snapshot.Directory, publishedRoot) {
		return Snapshot{}, fmt.Errorf("invalid independent published import path")
	}
	root, err := openPrivateDirectory(publishedRoot)
	if err != nil {
		return Snapshot{}, fmt.Errorf("published import root: %w", err)
	}
	defer root.Close()
	rootInfo, err := root.Stat(".")
	if err != nil {
		return Snapshot{}, err
	}
	index := make(map[string]Entry, len(snapshot.Entries))
	for _, entry := range snapshot.Entries {
		index[entry.Path] = entry
	}
	seen := make(map[string]bool, len(index))
	if err := inspectPublishedDirectory(root, publishedRoot, "", rootInfo, index, seen); err != nil {
		return Snapshot{}, err
	}
	if len(seen) != len(index) {
		return Snapshot{}, fmt.Errorf("published import tree omitted captured entries")
	}
	return snapshot, nil
}

func inspectPublishedDirectory(root *os.Root, publishedRoot, relative string, expected os.FileInfo, index map[string]Entry, seen map[string]bool) error {
	name := relative
	if name == "" {
		name = "."
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(opened, expected) || !sameSourceInfo(opened, expected) {
		return fmt.Errorf("published import directory changed while opening: %v", err)
	}
	directory, err := child.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(maxFiles + maxDirectories + 1)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	err = errors.Join(err, directory.Close())
	if err != nil || len(entries) > maxFiles+maxDirectories {
		return fmt.Errorf("read bounded published import directory: %v", err)
	}
	for _, entry := range entries {
		item := entry.Name()
		if relative != "" {
			item = path.Join(relative, item)
		}
		declared, exists := index[item]
		if !exists || seen[item] || entry.IsDir() != (declared.Kind == "directory") {
			return fmt.Errorf("published import contains an unlisted or changed entry")
		}
		listed, err := entry.Info()
		if err != nil {
			return err
		}
		info, err := root.Lstat(item)
		if err != nil || !os.SameFile(listed, info) || !sameSourceInfo(listed, info) {
			return fmt.Errorf("published import entry changed while inspecting: %v", err)
		}
		if err := admitSourceInfo(filepath.Join(publishedRoot, filepath.FromSlash(item)), info); err != nil {
			return err
		}
		if declared.Kind == "directory" {
			if !info.IsDir() || info.Mode().Perm() != 0o700 {
				return fmt.Errorf("published import directory is not private")
			}
			if err := inspectPublishedDirectory(root, publishedRoot, item, info, index, seen); err != nil {
				return err
			}
		} else {
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != declared.Size {
				return fmt.Errorf("published import file metadata differs")
			}
			if err := verifySnapshotFile(root, declared, info); err != nil {
				return err
			}
		}
		seen[item] = true
	}
	after, err := root.Lstat(name)
	if err != nil || !os.SameFile(expected, after) || !sameSourceInfo(expected, after) {
		return fmt.Errorf("published import directory changed during inspection: %v", err)
	}
	return nil
}
