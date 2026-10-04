package importx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

const MaxExclusions = 32
const maxSelectionBytes = 16 << 10

// Selection excludes literal relative paths and their subtrees. It has no
// implicit ignores, wildcard syntax or access to excluded file contents.
// ExpectedDigest optionally pins the complete preview manifest, not just names.
type Selection struct {
	Excludes       []string `json:"excludes"`
	ExpectedDigest string   `json:"expected_digest"`
}

func normalizeSelection(input Selection) (Selection, error) {
	if len(input.Excludes) > MaxExclusions || input.ExpectedDigest != "" && !validDigest(input.ExpectedDigest) {
		return Selection{}, fmt.Errorf("invalid import exclusion count or expected digest")
	}
	s := Selection{Excludes: append([]string{}, input.Excludes...), ExpectedDigest: input.ExpectedDigest}
	sort.Strings(s.Excludes)
	for i, name := range s.Excludes {
		if name == "" || len(name) > maxPathBytes || strings.HasPrefix(name, "/") || path.Clean(name) != name {
			return Selection{}, fmt.Errorf("invalid literal import exclusion %q", name)
		}
		components := strings.Split(name, "/")
		if len(components) > maxDepth+1 {
			return Selection{}, fmt.Errorf("import exclusion exceeds depth bound")
		}
		for _, c := range components {
			if c == "" || c == "." || c == ".." || len(c) > 63 {
				return Selection{}, fmt.Errorf("invalid import exclusion component")
			}
			for _, b := range c {
				if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '.' || b == '_' || b == '-') {
					return Selection{}, fmt.Errorf("import exclusions are literal safe relative paths")
				}
			}
		}
		// Reject redundancy rather than silently rewriting an operator's choices.
		for _, prior := range s.Excludes[:i] {
			if name == prior || strings.HasPrefix(name, prior+"/") {
				return Selection{}, fmt.Errorf("duplicate or overlapping import exclusion")
			}
		}
	}
	return s, nil
}

// CanonicalSelection is the small comparable bookmark value frozen before
// capture. Legacy bookmarks use an empty string and retain whole-tree behavior.
func CanonicalSelection(input Selection) (string, error) {
	s, err := normalizeSelection(input)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > maxSelectionBytes {
		return "", fmt.Errorf("import selection exceeds bound: %v", err)
	}
	return string(raw), nil
}

func ParseSelection(encoded string) (Selection, error) {
	if encoded == "" {
		return Selection{}, nil
	}
	if len(encoded) > maxSelectionBytes {
		return Selection{}, fmt.Errorf("import selection exceeds bound")
	}
	var s Selection
	if err := json.Unmarshal([]byte(encoded), &s); err != nil {
		return Selection{}, err
	}
	canonical, err := CanonicalSelection(s)
	// Exact canonical bytes reject missing/null/duplicate/aliased/unknown fields.
	if err != nil || canonical != encoded {
		return Selection{}, fmt.Errorf("noncanonical import selection: %v", err)
	}
	return normalizeSelection(s)
}

type exclusionState struct {
	selection Selection
	matched   map[string]bool
}

func newExclusionState(input Selection) (*exclusionState, error) {
	s, err := normalizeSelection(input)
	if err != nil {
		return nil, err
	}
	state := &exclusionState{selection: s, matched: make(map[string]bool, len(s.Excludes))}
	for _, name := range s.Excludes {
		state.matched[name] = false
	}
	return state, nil
}
func (s *exclusionState) exclude(name string) bool {
	if _, ok := s.matched[name]; ok {
		s.matched[name] = true
		return true
	}
	return false
}
func selectionManifest(snapshot *Snapshot, exclusions *exclusionState) ([]byte, error) {
	for _, name := range exclusions.selection.Excludes {
		if !exclusions.matched[name] {
			return nil, fmt.Errorf("import exclusion %q did not match; inspect the selection before importing", name)
		}
	}
	if snapshot.FileCount == 0 {
		return nil, fmt.Errorf("import source has no selected regular files")
	}
	raw, err := json.Marshal(struct {
		Version int     `json:"version"`
		Entries []Entry `json:"entries"`
	}{Version: 1, Entries: snapshot.Entries})
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > maxManifestBytes {
		return nil, fmt.Errorf("import manifest exceeds %d bytes", maxManifestBytes)
	}
	digest := sha256.Sum256(raw)
	snapshot.Digest = hex.EncodeToString(digest[:])
	if expected := exclusions.selection.ExpectedDigest; expected != "" && expected != snapshot.Digest {
		return nil, fmt.Errorf("import selection digest differs from preview; no snapshot published")
	}
	return raw, nil
}

// PreviewSource uses the capture walker and coherence checks without staging,
// journals, permissions changes or any guest operation. A later capture can pin
// its digest to reject additions, removals or edits since this observation.
func PreviewSource(ctx context.Context, sourcePath string, selection Selection) (Snapshot, error) {
	exclusions, err := newExclusionState(selection)
	if err != nil {
		return Snapshot{}, err
	}
	source, err := openPrivateDirectory(sourcePath)
	if err != nil {
		return Snapshot{}, fmt.Errorf("import source: %w", err)
	}
	defer source.Close()
	rootInfo, err := source.Stat(".")
	if err != nil {
		return Snapshot{}, err
	}
	var snapshot Snapshot
	if err := captureDirectory(ctx, source, nil, sourcePath, "", rootInfo, 0, &snapshot, exclusions); err != nil {
		return Snapshot{}, err
	}
	if _, err := selectionManifest(&snapshot, exclusions); err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}
