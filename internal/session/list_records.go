package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
)

// ListRecords reads the bounded, validated registry of one explicit domain.
// It does not claim that any recorded session is currently ready.
func ListRecords(stateRoot string, expectedDomain domain.ID) ([]Record, error) {
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return nil, err
	}
	root, err := openSessionStateRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("state root: %w", err)
	}
	defer root.Close()
	sessions, err := openSessionChild(root, "sessions", false)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("session directory: %w", err)
	}
	defer sessions.Close()
	directory, err := sessions.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(1025)
	closeErr := directory.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(entries) > 1024 {
		return nil, fmt.Errorf("session registry exceeds bound")
	}
	results := make([]Record, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && strings.Contains(name, ".tmp-") {
			continue
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".json") {
			return nil, fmt.Errorf("unexpected session registry entry %q", name)
		}
		parsed, err := ParseName(strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, fmt.Errorf("invalid session registry entry %q: %w", name, err)
		}
		record, err := loadRecordFromRoot(sessions, expectedDomain, parsed)
		if err != nil {
			return nil, fmt.Errorf("load session registry entry %q: %w", name, err)
		}
		results = append(results, record)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
	return results, nil
}
