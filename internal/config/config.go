package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const version = 1

type Config struct {
	domains map[domain.ID]Domain
}

type Domain struct {
	ID        domain.ID
	StateRoot string
}

func Load(path string) (Config, error) {
	if err := requireRegularFile(path); err != nil {
		return Config{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open configuration: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	loaded, err := decodeConfig(decoder)
	if err != nil {
		return Config{}, err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return Config{}, fmt.Errorf("read configuration tail: %w", err)
		}
		return Config{}, fmt.Errorf("unexpected configuration token %v", token)
	}
	return loaded, nil
}

func (c Config) Domain(raw string) (Domain, error) {
	id, err := domain.Parse(raw)
	if err != nil {
		return Domain{}, err
	}
	resolved, ok := c.domains[id]
	if !ok {
		return Domain{}, fmt.Errorf("unknown domain %q", raw)
	}
	return resolved, nil
}

func decodeConfig(decoder *json.Decoder) (Config, error) {
	if err := requireObjectStart(decoder); err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}

	seen := map[string]bool{}
	var gotVersion bool
	var gotDomains bool
	var parsedVersion int
	var domains map[domain.ID]Domain

	for decoder.More() {
		name, err := objectField(decoder, seen)
		if err != nil {
			return Config{}, fmt.Errorf("configuration: %w", err)
		}
		switch name {
		case "version":
			if err := decoder.Decode(&parsedVersion); err != nil {
				return Config{}, fmt.Errorf("configuration version: %w", err)
			}
			gotVersion = true
		case "domains":
			parsed, err := decodeDomains(decoder)
			if err != nil {
				return Config{}, err
			}
			domains = parsed
			gotDomains = true
		default:
			return Config{}, fmt.Errorf("unknown configuration field %q", name)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}
	if !gotVersion || parsedVersion != version {
		return Config{}, fmt.Errorf("unsupported configuration version %d", parsedVersion)
	}
	if !gotDomains || len(domains) == 0 {
		return Config{}, fmt.Errorf("configuration requires at least one domain")
	}
	if err := rejectOverlappingRoots(domains); err != nil {
		return Config{}, err
	}
	return Config{domains: domains}, nil
}

func decodeDomains(decoder *json.Decoder) (map[domain.ID]Domain, error) {
	if err := requireObjectStart(decoder); err != nil {
		return nil, fmt.Errorf("domains: %w", err)
	}

	domains := make(map[domain.ID]Domain)
	seen := map[string]bool{}
	for decoder.More() {
		rawID, err := objectField(decoder, seen)
		if err != nil {
			return nil, fmt.Errorf("domains: %w", err)
		}
		id, err := domain.Parse(rawID)
		if err != nil {
			return nil, err
		}
		entry, err := decodeDomain(decoder, id)
		if err != nil {
			return nil, err
		}
		domains[id] = entry
	}
	if err := requireObjectEnd(decoder); err != nil {
		return nil, fmt.Errorf("domains: %w", err)
	}
	return domains, nil
}

func decodeDomain(decoder *json.Decoder, id domain.ID) (Domain, error) {
	if err := requireObjectStart(decoder); err != nil {
		return Domain{}, fmt.Errorf("domain %q: %w", id, err)
	}

	seen := map[string]bool{}
	var root string
	var gotRoot bool
	for decoder.More() {
		name, err := objectField(decoder, seen)
		if err != nil {
			return Domain{}, fmt.Errorf("domain %q: %w", id, err)
		}
		if name != "state_root" {
			return Domain{}, fmt.Errorf("domain %q: unknown field %q", id, name)
		}
		if err := decoder.Decode(&root); err != nil {
			return Domain{}, fmt.Errorf("domain %q state_root: %w", id, err)
		}
		gotRoot = true
	}
	if err := requireObjectEnd(decoder); err != nil {
		return Domain{}, fmt.Errorf("domain %q: %w", id, err)
	}
	if !gotRoot {
		return Domain{}, fmt.Errorf("domain %q requires state_root", id)
	}
	canonical, err := canonicalPrivateDirectory(root)
	if err != nil {
		return Domain{}, fmt.Errorf("domain %q state_root: %w", id, err)
	}
	return Domain{ID: id, StateRoot: canonical}, nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect configuration: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("configuration must be a regular non-symlink file")
	}
	return nil
}

func canonicalPrivateDirectory(raw string) (string, error) {
	if !filepath.IsAbs(raw) {
		return "", fmt.Errorf("must be absolute")
	}
	clean := filepath.Clean(raw)
	if clean != raw {
		return "", fmt.Errorf("must be canonical")
	}
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", fmt.Errorf("resolve: %w", err)
	}
	if canonical != clean {
		return "", fmt.Errorf("must not contain a symlink")
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("must be a directory")
	}
	if info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("must have mode 0700")
	}
	return canonical, nil
}

func rejectOverlappingRoots(domains map[domain.ID]Domain) error {
	for firstID, first := range domains {
		for secondID, second := range domains {
			if firstID >= secondID {
				continue
			}
			overlap, err := isSameOrPhysicalDescendant(first.StateRoot, second.StateRoot)
			if err != nil {
				return fmt.Errorf("compare domain roots %q and %q: %w", firstID, secondID, err)
			}
			if !overlap {
				overlap, err = isSameOrPhysicalDescendant(second.StateRoot, first.StateRoot)
				if err != nil {
					return fmt.Errorf("compare domain roots %q and %q: %w", firstID, secondID, err)
				}
			}
			if overlap {
				return fmt.Errorf("domain roots %q and %q overlap", firstID, secondID)
			}
		}
	}
	return nil
}

func isSameOrPhysicalDescendant(parent, child string) (bool, error) {
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return false, err
	}
	for current := child; ; current = filepath.Dir(current) {
		currentInfo, err := os.Stat(current)
		if err != nil {
			return false, err
		}
		if os.SameFile(parentInfo, currentInfo) {
			return true, nil
		}
		next := filepath.Dir(current)
		if next == current {
			return false, nil
		}
	}
}

func requireObjectStart(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return fmt.Errorf("expected object")
	}
	return nil
}

func requireObjectEnd(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '}' {
		return fmt.Errorf("expected object end")
	}
	return nil
}

func objectField(decoder *json.Decoder, seen map[string]bool) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	name, ok := token.(string)
	if !ok {
		return "", fmt.Errorf("expected object field")
	}
	if seen[name] {
		return "", fmt.Errorf("duplicate field %q", name)
	}
	seen[name] = true
	return name, nil
}
