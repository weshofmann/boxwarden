package workspacex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const maxExportListingEntries = 4096
const maxExportListingJournals = 1024

// ListExportJournals is an unlocked read-only snapshot of validated private
// metadata for one volume. Recorded phases and paths are locators, not proof of
// current disk state, helper lifetime or publication. It does not open snapshots,
// destinations, workspace/session records, locks or recovery operations.
func ListExportJournals(ctx context.Context, stateRoot string, expectedDomain domain.ID, volumeID string) ([]ExportJournal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := domain.Parse(string(expectedDomain)); err != nil {
		return nil, err
	}
	if !validUUID(volumeID) {
		return nil, errors.New("invalid export listing volume UUID")
	}
	root, err := openStateRoot(stateRoot)
	if err != nil {
		return nil, fmt.Errorf("export state root: %w", err)
	}
	defer root.Close()
	exports, err := openChild(root, "exports", false)
	if errors.Is(err, os.ErrNotExist) {
		return []ExportJournal{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer exports.Close()
	file, err := exports.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := file.ReadDir(maxExportListingEntries + 1)
	closeErr := file.Close()
	if errors.Is(readErr, io.EOF) {
		readErr = nil
	}
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	if len(entries) > maxExportListingEntries {
		return nil, errors.New("export registry exceeds 4096 entries")
	}
	results := []ExportJournal{}
	journals := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if exportListingTemporary(name) {
			// A legitimate interrupted atomic writer may leave incomplete bytes.
			// Admit only its exact private one-link name and bounded metadata;
			// never parse it as a transaction, remove it, or complete publication.
			file, err := openPrivateFile(exports, name)
			if err != nil {
				return nil, fmt.Errorf("export temporary %q: %w", name, err)
			}
			info, statErr := file.Stat()
			closeErr := file.Close()
			if err := errors.Join(statErr, closeErr); err != nil {
				return nil, err
			}
			if info.Size() < 0 || info.Size() > maxExportJournalBytes {
				return nil, errors.New("export temporary exceeds journal bound")
			}
			continue
		}
		if validUUID(name) {
			child, err := openChild(exports, name, false)
			if err != nil {
				return nil, fmt.Errorf("export transaction directory %q: %w", name, err)
			}
			if err := child.Close(); err != nil {
				return nil, err
			}
			continue
		}
		if !strings.HasSuffix(name, ".json") || !validUUID(strings.TrimSuffix(name, ".json")) {
			return nil, fmt.Errorf("unexpected export registry entry %q", name)
		}
		journals++
		if journals > maxExportListingJournals {
			return nil, errors.New("export registry exceeds 1024 journals")
		}
		journal, err := loadExportJournalFromRoot(exports, expectedDomain, strings.TrimSuffix(name, ".json"))
		if err != nil {
			return nil, fmt.Errorf("load export %q: %w", name, err)
		}
		if journal.VolumeID == volumeID {
			results = append(results, journal)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
	return results, nil
}

func exportListingTemporary(name string) bool {
	id, nonce, ok := strings.Cut(name, ".json.tmp-")
	if !ok || !validUUID(id) || len(nonce) != 32 {
		return false
	}
	for _, c := range nonce {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
