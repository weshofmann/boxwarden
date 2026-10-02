package projectx

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
)

// List reads validated bookmarks without creating directories, locks, or
// receipts. An absent projects child is empty only after the state root itself
// has been admitted; an unavailable root must never become an empty registry.
func List(stateRoot string, expectedDomain domain.ID) ([]Record, error) {
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return nil, err
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("project state root: %w", err)
	}
	defer root.Close()
	dir, err := openChild(root, "projects", false)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	f, err := dir.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := f.ReadDir(1025)
	closeErr := f.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(entries) > 1024 {
		return nil, errors.New("project registry exceeds 1024 entries")
	}
	results := make([]Record, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == setupName {
			continue
		}
		// Atomic publications can leave private temporary files after interruption.
		if strings.Contains(name, ".json.tmp-") {
			continue
		}
		if history, err := admitSetupHistory(dir, name); err != nil {
			return nil, fmt.Errorf("project setup history %q: %w", name, err)
		} else if history {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("unexpected project registry entry %q", name)
		}
		key := strings.TrimSuffix(name, ".json")
		if err := validateKey(expectedDomain, key); err != nil {
			return nil, fmt.Errorf("invalid project registry key %q: %w", name, err)
		}
		record, _, err := loadRecord(dir, expectedDomain, key)
		if err != nil {
			return nil, fmt.Errorf("load project %q: %w", key, err)
		}
		results = append(results, record)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}
