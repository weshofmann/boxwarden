// Package recipe loads the bounded, declarative alpha environment description.
// Command vectors in a recipe are guest intent; this package never executes
// them on the trusted host.
package recipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"unicode"
)

const (
	maxRecipeBytes = 1 << 20
	sourceKind     = "ubuntu-24.04.4-desktop-arm64"
	sourceSHA256   = "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"
	sourceBytes    = 3540299776
	workspaceRoot  = "/home/boxwarden/workspaces/"
)

// VerifyISO checks the one installer admitted by the alpha recipe. The
// published Canonical checksum is pinned in Source.SHA256; this checks the
// selected local bytes before they enter the preparation path.
func VerifyISO(filename string) error {
	return verifyDigest(filename, sourceSHA256, sourceBytes)
}

func verifyDigest(filename, expected string, expectedSize int64) error {
	entry, err := os.Lstat(filename)
	if err != nil {
		return fmt.Errorf("inspect installer: %w", err)
	}
	if !entry.Mode().IsRegular() {
		return errors.New("installer must be a regular file, not a link or directory")
	}
	file, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open installer: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat installer: %w", err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(entry, opened) || opened.Size() != expectedSize {
		return errors.New("installer identity, type, or length differs from the pinned source")
	}
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return fmt.Errorf("hash installer: %w", err)
	}
	if got := fmt.Sprintf("%x", digest.Sum(nil)); got != expected {
		return fmt.Errorf("installer SHA-256 %s differs from pinned %s", got, expected)
	}
	return nil
}

type Recipe struct {
	Version     int         `json:"version"`
	Source      Source      `json:"source"`
	Machine     Machine     `json:"machine"`
	AptPackages []string    `json:"apt_packages"`
	Steps       []Step      `json:"steps"`
	Workspaces  []Workspace `json:"workspaces"`
	Launch      []Launch    `json:"launch"`
}

type Source struct {
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
}

type Machine struct {
	CPUs          int `json:"cpus"`
	MemoryMiB     int `json:"memory_mib"`
	SystemDiskGiB int `json:"system_disk_gib"`
}

type Step struct {
	ID    string   `json:"id"`
	Phase string   `json:"phase"`
	Argv  []string `json:"argv"`
}

type Workspace struct {
	Name  string `json:"name"`
	Mount string `json:"mount"`
}

type Launch struct {
	ID   string   `json:"id"`
	Argv []string `json:"argv"`
}

// Load reads one exact versioned recipe. A selected installer is supplied
// separately and must match Source.SHA256 before preparation uses it.
func Load(filename string) (Recipe, error) {
	file, err := os.Open(filename)
	if err != nil {
		return Recipe{}, fmt.Errorf("open recipe: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRecipeBytes+1))
	if err != nil {
		return Recipe{}, fmt.Errorf("read recipe: %w", err)
	}
	if len(data) > maxRecipeBytes {
		return Recipe{}, fmt.Errorf("recipe exceeds %d bytes", maxRecipeBytes)
	}
	if err := rejectDuplicateKeys(data); err != nil {
		return Recipe{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value Recipe
	if err := decoder.Decode(&value); err != nil {
		return Recipe{}, fmt.Errorf("decode recipe: %w", err)
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Recipe{}, fmt.Errorf("recipe must contain one JSON object")
	}
	if err := value.validate(); err != nil {
		return Recipe{}, err
	}
	return value, nil
}

// LoadRunnable admits only actions the current alpha commands can execute.
// Reconfigure remains explicit; automatic once/startup and graphical launch
// require their own lifecycle integration. Load can inspect the full format.
func LoadRunnable(filename string) (Recipe, error) {
	value, err := Load(filename)
	if err != nil {
		return Recipe{}, err
	}
	for _, step := range value.Steps {
		if step.Phase != "prepare" && step.Phase != "reconfigure" {
			return Recipe{}, fmt.Errorf("recipe phase %q is unsupported until session execution is available", step.Phase)
		}
	}
	if len(value.Launch) != 0 {
		return Recipe{}, errors.New("recipe launch actions are unsupported until session execution is available")
	}
	return value, nil
}

func (r Recipe) validate() error {
	if r.Version != 1 || r.Source.Kind != sourceKind || r.Source.SHA256 != sourceSHA256 {
		return fmt.Errorf("unsupported recipe version or installer source")
	}
	if r.Machine.CPUs < 1 || r.Machine.CPUs > 16 || r.Machine.MemoryMiB < 2048 || r.Machine.MemoryMiB > 65536 || r.Machine.SystemDiskGiB < 20 || r.Machine.SystemDiskGiB > 2048 {
		return fmt.Errorf("machine resources are outside supported bounds")
	}
	if len(r.AptPackages) > 128 || len(r.Steps) > 128 || len(r.Workspaces) > 16 || len(r.Launch) > 16 {
		return fmt.Errorf("recipe item count exceeds bound")
	}
	packages := make(map[string]bool, len(r.AptPackages))
	for _, pkg := range r.AptPackages {
		if !validPackage(pkg) || packages[pkg] {
			return fmt.Errorf("invalid or duplicate apt package %q", pkg)
		}
		packages[pkg] = true
	}
	ids := make(map[string]bool, len(r.Steps)+len(r.Launch))
	for _, step := range r.Steps {
		if !validID(step.ID) || ids[step.ID] || !validPhase(step.Phase) || !validArgv(step.Argv) {
			return fmt.Errorf("invalid or duplicate guest step %q", step.ID)
		}
		ids[step.ID] = true
	}
	for _, launch := range r.Launch {
		if !validID(launch.ID) || ids[launch.ID] || !validArgv(launch.Argv) {
			return fmt.Errorf("invalid or duplicate launch action %q", launch.ID)
		}
		ids[launch.ID] = true
	}
	volumes := make(map[string]bool, len(r.Workspaces))
	mounts := make([]string, 0, len(r.Workspaces))
	for _, workspace := range r.Workspaces {
		if !validID(workspace.Name) || volumes[workspace.Name] || !validMount(workspace.Mount) {
			return fmt.Errorf("invalid or duplicate workspace %q", workspace.Name)
		}
		for _, mount := range mounts {
			if workspace.Mount == mount || strings.HasPrefix(workspace.Mount, mount+"/") || strings.HasPrefix(mount, workspace.Mount+"/") {
				return fmt.Errorf("overlapping workspace mounts")
			}
		}
		volumes[workspace.Name] = true
		mounts = append(mounts, workspace.Mount)
	}
	return nil
}

func validID(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

func validPackage(value string) bool {
	if len(value) < 1 || len(value) > 128 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, r := range value[1:] {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '+' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func validPhase(value string) bool {
	switch value {
	case "prepare", "once", "reconfigure", "startup":
		return true
	default:
		return false
	}
}

func validArgv(args []string) bool {
	if len(args) < 1 || len(args) > 32 || args[0] == "" {
		return false
	}
	for _, arg := range args {
		if len(arg) > 65536 || strings.IndexFunc(arg, func(r rune) bool {
			return unicode.IsControl(r) && r != '\n' && r != '\t'
		}) >= 0 {
			return false
		}
	}
	return true
}

func validMount(mount string) bool {
	return strings.HasPrefix(mount, workspaceRoot) && len(mount) > len(workspaceRoot) && path.Clean(mount) == mount && !strings.Contains(mount, "//") && strings.IndexFunc(mount, unicode.IsControl) < 0
}

// A second token pass rejects duplicate keys before the ordinary strict JSON
// decoder maps them onto a Go struct. Otherwise later fields silently win.
func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := checkJSONValue(decoder); err != nil {
		return fmt.Errorf("recipe JSON: %w", err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("recipe must contain one JSON object")
	}
	return nil
}

func checkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok || seen[key] {
				return fmt.Errorf("duplicate or invalid object key %q", key)
			}
			seen[key] = true
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := checkJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delim)
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	return nil
}
