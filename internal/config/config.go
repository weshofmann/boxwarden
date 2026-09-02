package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
)

const (
	legacyVersion = 1
	version       = 2
)

type Config struct {
	domains map[domain.ID]Domain
	host    *Host
}

// Host contains declared, unprivileged input locations. Their qualified
// identities are validated by hostx doctor/init after configuration admission.
type Host struct {
	TartExecutable string
	TartHome       string
	SoftnetSource  string
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

// Domains returns a deterministic snapshot of the configured domains.
func (c Config) Domains() []Domain {
	domains := make([]Domain, 0, len(c.domains))
	for _, configured := range c.domains {
		domains = append(domains, configured)
	}
	sort.Slice(domains, func(i, j int) bool { return domains[i].ID < domains[j].ID })
	return domains
}

// Host returns the V3 host prerequisite declaration. Version-1 configuration
// remains readable for V1/V2 commands but cannot authorize V3 operations.
func (c Config) Host() (Host, error) {
	if c.host == nil {
		return Host{}, fmt.Errorf("configuration version 1 has no V3 host prerequisites; migrate to version 2")
	}
	return *c.host, nil
}

func decodeConfig(decoder *json.Decoder) (Config, error) {
	if err := requireObjectStart(decoder); err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}

	seen := map[string]bool{}
	var gotVersion bool
	var gotDomains bool
	var gotHost bool
	var parsedVersion int
	var domains map[domain.ID]Domain
	var host Host

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
		case "host":
			parsed, err := decodeHost(decoder)
			if err != nil {
				return Config{}, err
			}
			host = parsed
			gotHost = true
		default:
			return Config{}, fmt.Errorf("unknown configuration field %q", name)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}
	if !gotVersion || (parsedVersion != legacyVersion && parsedVersion != version) {
		return Config{}, fmt.Errorf("unsupported configuration version %d", parsedVersion)
	}
	if !gotDomains || len(domains) == 0 {
		return Config{}, fmt.Errorf("configuration requires at least one domain")
	}
	if err := rejectOverlappingRoots(domains); err != nil {
		return Config{}, err
	}
	if parsedVersion == version && !gotHost {
		return Config{}, fmt.Errorf("configuration version 2 requires host prerequisites")
	}
	if parsedVersion == legacyVersion && gotHost {
		return Config{}, fmt.Errorf("configuration version 1 does not support host prerequisites")
	}
	if parsedVersion == version {
		if err := rejectHostDomainOverlap(host, domains); err != nil {
			return Config{}, err
		}
	}
	loaded := Config{domains: domains}
	if gotHost {
		loaded.host = &host
	}
	return loaded, nil
}

func decodeHost(decoder *json.Decoder) (Host, error) {
	if err := requireObjectStart(decoder); err != nil {
		return Host{}, fmt.Errorf("host: %w", err)
	}
	seen := map[string]bool{}
	var host Host
	var gotTartExecutable bool
	var gotTartHome bool
	var gotSoftnetSource bool
	for decoder.More() {
		name, err := objectField(decoder, seen)
		if err != nil {
			return Host{}, fmt.Errorf("host: %w", err)
		}
		var target *string
		switch name {
		case "tart_executable":
			target = &host.TartExecutable
			gotTartExecutable = true
		case "tart_home":
			target = &host.TartHome
			gotTartHome = true
		case "softnet_source":
			target = &host.SoftnetSource
			gotSoftnetSource = true
		default:
			return Host{}, fmt.Errorf("host: unknown field %q", name)
		}
		if err := decoder.Decode(target); err != nil {
			return Host{}, fmt.Errorf("host %s: %w", name, err)
		}
	}
	if err := requireObjectEnd(decoder); err != nil {
		return Host{}, fmt.Errorf("host: %w", err)
	}
	if !gotTartExecutable || !gotTartHome || !gotSoftnetSource {
		return Host{}, fmt.Errorf("host requires tart_executable, tart_home, and softnet_source")
	}
	var err error
	host.TartExecutable, err = canonicalRegularFile(host.TartExecutable)
	if err != nil {
		return Host{}, fmt.Errorf("host tart_executable: %w", err)
	}
	host.TartHome, err = canonicalPrivateOperatorDirectory(host.TartHome)
	if err != nil {
		return Host{}, fmt.Errorf("host tart_home: %w", err)
	}
	host.SoftnetSource, err = canonicalRegularFile(host.SoftnetSource)
	if err != nil {
		return Host{}, fmt.Errorf("host softnet_source: %w", err)
	}
	return host, nil
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
	canonical, info, err := canonicalExistingPath(raw)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("must be a directory")
	}
	if info.Mode().Perm() != 0o700 {
		return "", fmt.Errorf("must have mode 0700")
	}
	return canonical, nil
}

func canonicalPrivateOperatorDirectory(raw string) (string, error) {
	canonical, err := canonicalPrivateDirectory(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != os.Getuid() {
		return "", fmt.Errorf("must be owned by the current operator")
	}
	return canonical, nil
}

func canonicalRegularFile(raw string) (string, error) {
	canonical, info, err := canonicalExistingPath(raw)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("must be a regular file")
	}
	return canonical, nil
}

func canonicalExistingPath(raw string) (string, os.FileInfo, error) {
	if !filepath.IsAbs(raw) {
		return "", nil, fmt.Errorf("must be absolute")
	}
	clean := filepath.Clean(raw)
	if clean != raw || clean == "/" {
		return "", nil, fmt.Errorf("must be canonical")
	}
	canonical, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return "", nil, fmt.Errorf("resolve: %w", err)
	}
	if canonical != clean {
		return "", nil, fmt.Errorf("must not contain a symlink")
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return "", nil, fmt.Errorf("inspect: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", nil, fmt.Errorf("must not contain a symlink")
	}
	return canonical, info, nil
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

func rejectHostDomainOverlap(host Host, domains map[domain.ID]Domain) error {
	for id, configured := range domains {
		for name, path := range map[string]string{
			"tart_executable": host.TartExecutable,
			"tart_home":       host.TartHome,
			"softnet_source":  host.SoftnetSource,
		} {
			overlap, err := isSameOrPhysicalDescendant(configured.StateRoot, path)
			if err != nil {
				return fmt.Errorf("compare domain root %q and host %s: %w", id, name, err)
			}
			if !overlap {
				overlap, err = isSameOrPhysicalDescendant(path, configured.StateRoot)
				if err != nil {
					return fmt.Errorf("compare host %s and domain root %q: %w", name, id, err)
				}
			}
			if overlap {
				return fmt.Errorf("host %s overlaps domain root %q", name, id)
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
