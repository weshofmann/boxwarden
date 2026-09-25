package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// ListActionAttempts reads the exact session's bounded durable journal. It
// reports stored outcomes only; an indeterminate entry is not guest evidence.
func ListActionAttempts(ctx context.Context, configured config.Domain, rawName string) (results []ActionAttempt, err error) {
	domainID, parseErr := domain.Parse(string(configured.ID))
	if parseErr != nil || domainID != configured.ID || strings.TrimSpace(configured.StateRoot) == "" {
		return nil, fmt.Errorf("invalid configured domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return nil, err
	}
	held, err := lock.AcquireSession(ctx, configured.StateRoot, string(domainID), string(name))
	if err != nil {
		return nil, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	record, err := LoadRecord(configured.StateRoot, string(domainID), string(name))
	if err != nil {
		return nil, err
	}
	root, err := openSessionStateRoot(configured.StateRoot)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	attempts, err := openSessionChild(root, "action-attempts", false)
	if errors.Is(err, os.ErrNotExist) {
		return []ActionAttempt{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer attempts.Close()
	sessionAttempts, err := openSessionChild(attempts, record.ID, false)
	if errors.Is(err, os.ErrNotExist) {
		return []ActionAttempt{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer sessionAttempts.Close()
	directory, err := sessionAttempts.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(4097)
	closeErr := directory.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("action attempt registry exceeds bound")
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil, fmt.Errorf("unexpected action attempt entry %q", entry.Name())
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		if !validUUID(id) {
			return nil, fmt.Errorf("invalid action attempt entry %q", entry.Name())
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	results = make([]ActionAttempt, 0, len(ids))
	for _, id := range ids {
		attempt, loadErr := LoadActionAttempt(configured.StateRoot, domainID, record.ID, id)
		if loadErr != nil {
			return nil, fmt.Errorf("load exact action attempt %q: %w", id, loadErr)
		}
		if attempt.SessionName != string(name) {
			return nil, fmt.Errorf("action attempt %q names a different session", id)
		}
		results = append(results, attempt)
	}
	return results, nil
}
