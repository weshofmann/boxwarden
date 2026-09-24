package importx

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const maxManifestBytes = 256 << 10

var beforeSnapshotWalk = func() {}

// InspectSnapshot re-admits the exact private bytes before a transfer or a
// retry. A previous success bit or manifest alone is never file evidence.
func InspectSnapshot(stagingParent, transactionID string) (Snapshot, error) {
	if !canonicalAbsolute(stagingParent) || !validUUID(transactionID) {
		return Snapshot{}, fmt.Errorf("invalid import snapshot identity")
	}
	parent, err := openPrivateDirectory(stagingParent)
	if err != nil {
		return Snapshot{}, err
	}
	defer parent.Close()
	directory := filepath.Join(stagingParent, transactionID)
	rootInfo, err := parent.Lstat(transactionID)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		return Snapshot{}, fmt.Errorf("invalid import snapshot directory: %v", err)
	}
	if err := admitSourceInfo(directory, rootInfo); err != nil {
		return Snapshot{}, err
	}
	root, err := parent.OpenRoot(transactionID)
	if err != nil {
		return Snapshot{}, err
	}
	defer root.Close()
	openedRoot, err := root.Stat(".")
	if err != nil || !os.SameFile(rootInfo, openedRoot) {
		return Snapshot{}, fmt.Errorf("import snapshot directory changed while opening: %v", err)
	}
	manifestInfo, err := root.Lstat(manifestName)
	if err != nil || !manifestInfo.Mode().IsRegular() || manifestInfo.Mode().Perm() != 0o600 || manifestInfo.Size() < 2 || manifestInfo.Size() > maxManifestBytes {
		return Snapshot{}, fmt.Errorf("invalid import manifest metadata: %v", err)
	}
	if err := admitSourceInfo(filepath.Join(directory, manifestName), manifestInfo); err != nil {
		return Snapshot{}, err
	}
	file, err := root.OpenFile(manifestName, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Snapshot{}, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(manifestInfo, opened) {
		file.Close()
		return Snapshot{}, fmt.Errorf("import manifest changed while opening: %v", err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, maxManifestBytes+1))
	afterManifest, statErr := root.Lstat(manifestName)
	if statErr != nil || !os.SameFile(manifestInfo, afterManifest) || !sameSourceInfo(manifestInfo, afterManifest) {
		file.Close()
		return Snapshot{}, fmt.Errorf("import manifest changed during admission: %v", statErr)
	}
	err = errors.Join(err, file.Close())
	if err != nil || len(raw) > maxManifestBytes {
		return Snapshot{}, fmt.Errorf("read bounded import manifest: %v", err)
	}
	var manifest struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}
	if err := json.Unmarshal(bytes.TrimSuffix(raw, []byte{'\n'}), &manifest); err != nil || manifest.Version != 1 {
		return Snapshot{}, fmt.Errorf("invalid import manifest: %v", err)
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return Snapshot{}, fmt.Errorf("import manifest is not canonical")
	}
	if len(manifest.Entries) == 0 || len(manifest.Entries) > maxFiles+maxDirectories {
		return Snapshot{}, fmt.Errorf("invalid import entry count")
	}
	snapshot := Snapshot{TransactionID: transactionID, Directory: directory, Entries: manifest.Entries}
	index := make(map[string]Entry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		if !validSnapshotPath(entry.Path) || entry.Path == manifestName {
			return Snapshot{}, fmt.Errorf("invalid import entry path")
		}
		if _, exists := index[entry.Path]; exists {
			return Snapshot{}, fmt.Errorf("duplicate import entry path")
		}
		parentPath := path.Dir(entry.Path)
		if parentPath != "." {
			ancestor, exists := index[parentPath]
			if !exists || ancestor.Kind != "directory" {
				return Snapshot{}, fmt.Errorf("import entry lacks declared directory ancestor")
			}
		}
		info, err := root.Lstat(entry.Path)
		if err != nil {
			return Snapshot{}, err
		}
		if err := admitSourceInfo(filepath.Join(directory, filepath.FromSlash(entry.Path)), info); err != nil {
			return Snapshot{}, err
		}
		switch entry.Kind {
		case "directory":
			if !info.IsDir() || info.Mode().Perm() != 0o700 || entry.Size != 0 || entry.SHA256 != "" || snapshot.DirectoryCount >= maxDirectories {
				return Snapshot{}, fmt.Errorf("invalid import directory entry")
			}
			snapshot.DirectoryCount++
		case "file":
			if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != entry.Size || entry.Size < 0 || entry.Size > maxFileBytes ||
				snapshot.FileCount >= maxFiles || snapshot.TotalBytes > maxTotalBytes-entry.Size || !validDigest(entry.SHA256) {
				return Snapshot{}, fmt.Errorf("invalid import file entry")
			}
			if err := verifySnapshotFile(root, entry, info); err != nil {
				return Snapshot{}, err
			}
			snapshot.FileCount++
			snapshot.TotalBytes += entry.Size
		default:
			return Snapshot{}, fmt.Errorf("invalid import entry kind")
		}
		index[entry.Path] = entry
	}
	if snapshot.FileCount == 0 {
		return Snapshot{}, fmt.Errorf("empty import snapshot")
	}
	beforeSnapshotWalk()
	seen := make(map[string]bool, len(index))
	if err := inspectSnapshotDirectory(root, "", index, seen); err != nil {
		return Snapshot{}, err
	}
	if len(seen) != len(index) {
		return Snapshot{}, fmt.Errorf("declared import snapshot entry disappeared during admission")
	}
	digest := sha256.Sum256(canonical)
	snapshot.Digest = hex.EncodeToString(digest[:])
	return snapshot, nil
}

func validSnapshotPath(value string) bool {
	if len(value) == 0 || len(value) > maxPathBytes || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	components := strings.Split(value, "/")
	if len(components) > maxDepth+1 {
		return false
	}
	for _, component := range components {
		if !validComponent(component) {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
			return false
		}
	}
	return true
}

func verifySnapshotFile(root *os.Root, entry Entry, expected os.FileInfo) error {
	file, err := root.OpenFile(entry.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(expected, opened) || !sameSourceInfo(expected, opened) {
		return fmt.Errorf("import snapshot file changed while opening: %v", err)
	}
	hasher := sha256.New()
	if _, err := io.CopyN(hasher, file, entry.Size); err != nil {
		return err
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); count != 0 || err != io.EOF {
		return fmt.Errorf("import snapshot file size changed during admission: %v", err)
	}
	after, err := root.Lstat(entry.Path)
	if err != nil || !os.SameFile(expected, after) || !sameSourceInfo(expected, after) || hex.EncodeToString(hasher.Sum(nil)) != entry.SHA256 {
		return fmt.Errorf("import snapshot file digest or identity changed: %v", err)
	}
	return nil
}

func inspectSnapshotDirectory(root *os.Root, relative string, index map[string]Entry, seen map[string]bool) error {
	name := relative
	if name == "" {
		name = "."
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return err
	}
	defer child.Close()
	directory, err := child.Open(".")
	if err != nil {
		return err
	}
	entries, err := directory.ReadDir(maxFiles + maxDirectories + 2)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	err = errors.Join(err, directory.Close())
	if err != nil || len(entries) > maxFiles+maxDirectories+1 {
		return fmt.Errorf("read bounded import snapshot directory: %v", err)
	}
	for _, entry := range entries {
		if relative == "" && entry.Name() == manifestName {
			continue
		}
		name := entry.Name()
		if relative != "" {
			name = path.Join(relative, name)
		}
		declared, exists := index[name]
		if !exists || seen[name] || (entry.IsDir() != (declared.Kind == "directory")) {
			return fmt.Errorf("unlisted or changed import snapshot entry")
		}
		seen[name] = true
		if declared.Kind == "directory" {
			if err := inspectSnapshotDirectory(root, name, index, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
