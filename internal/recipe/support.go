package recipe

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	GuestSupportPrepareID = "boxwarden-support-prepare"
	GuestSupportStartupID = "boxwarden-support-startup"
	GuestSupportCheckPath = "/usr/local/libexec/boxwarden-guest-support-check"
)

// WithGuestSupport captures fixed support checks from the same source snapshot
// used to prepare a new system. It never changes the caller's recipe or executes
// guest code. Reserved names cannot be supplied by an operator recipe.
func WithGuestSupport(value Recipe, definitionRoot string) (Recipe, error) {
	if err := value.validate(); err != nil {
		return Recipe{}, err
	}
	for _, step := range value.Steps {
		if strings.HasPrefix(step.ID, "boxwarden-support-") {
			return Recipe{}, fmt.Errorf("recipe uses a reserved guest support action")
		}
	}
	for _, launch := range value.Launch {
		if strings.HasPrefix(launch.ID, "boxwarden-support-") {
			return Recipe{}, fmt.Errorf("recipe uses a reserved guest support action")
		}
	}
	if !filepath.IsAbs(definitionRoot) || filepath.Clean(definitionRoot) != definitionRoot {
		return Recipe{}, fmt.Errorf("guest support definition root must be canonical and absolute")
	}
	lock, _, err := supportSource(definitionRoot, "artifacts.lock.json", 1<<20)
	if err != nil {
		return Recipe{}, err
	}
	var pins struct {
		Version   int `json:"version"`
		Artifacts map[string]struct {
			SHA256    string `json:"sha256"`
			GoVersion string `json:"go_version"`
			Build     string `json:"build"`
			ELF       struct {
				Machine  string `json:"machine"`
				PTInterp bool   `json:"pt_interp"`
				DTNeeded bool   `json:"dt_needed"`
			} `json:"elf"`
		} `json:"artifacts"`
	}
	decoder := json.NewDecoder(bytes.NewReader(lock))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&pins); err != nil {
		return Recipe{}, fmt.Errorf("invalid guest support artifact lock")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || pins.Version != 1 || len(pins.Artifacts) != 1 {
		return Recipe{}, fmt.Errorf("invalid guest support artifact lock")
	}
	bootstrap, ok := pins.Artifacts["boxwarden-guest-bootstrap"]
	if !ok || bootstrap.GoVersion != "go1.27.0" || bootstrap.ELF.Machine != "EM_AARCH64" || bootstrap.ELF.PTInterp || bootstrap.ELF.DTNeeded {
		return Recipe{}, fmt.Errorf("unsupported guest support artifact compatibility")
	}
	_, helperDigest, err := supportSource(definitionRoot, "artifacts/boxwarden-guest-bootstrap", 16<<20)
	if err != nil {
		return Recipe{}, err
	}
	if helperDigest != bootstrap.SHA256 {
		return Recipe{}, fmt.Errorf("guest support bootstrap differs from artifact lock")
	}
	_, adapterDigest, err := supportSource(definitionRoot, "clipboard.py", 1<<20)
	if err != nil {
		return Recipe{}, err
	}
	checker, checkerDigest, err := supportSource(definitionRoot, "support-check.py", 1<<20)
	if err != nil {
		return Recipe{}, err
	}
	if !bytes.Contains(checker, []byte("\nSUPPORT_VERSION = 1\n")) {
		return Recipe{}, fmt.Errorf("unsupported guest support checker compatibility")
	}
	argv := func(mode string) []string {
		return []string{"/usr/bin/python3", GuestSupportCheckPath, mode, "1", helperDigest, adapterDigest, checkerDigest}
	}
	prepare := Step{ID: GuestSupportPrepareID, Phase: "prepare", Argv: argv("--prepare")}
	startup := Step{ID: GuestSupportStartupID, Phase: "startup", Argv: append([]string{"/usr/bin/sudo", "-n", "--"}, argv("--runtime")...)}
	value.Steps = append([]Step{prepare, startup}, value.Steps...)
	if err := value.validate(); err != nil {
		return Recipe{}, fmt.Errorf("guest support recipe exceeds supported bounds: %w", err)
	}
	return value, nil
}

func supportSource(root, name string, maximum int64) ([]byte, string, error) {
	filename := filepath.Join(root, name)
	resolved, err := filepath.EvalSymlinks(filename)
	if err != nil || resolved != filename {
		return nil, "", fmt.Errorf("guest support source %s is missing or has a symlinked path", name)
	}
	before, err := os.Lstat(filename)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > maximum || before.Mode().Perm()&022 != 0 {
		return nil, "", fmt.Errorf("guest support source %s has unsafe type, size or permissions", name)
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, "", fmt.Errorf("open guest support source %s: %w", name, err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, "", fmt.Errorf("guest support source %s changed while opening", name)
	}
	data, err := io.ReadAll(io.LimitReader(file, maximum+1))
	after, statErr := file.Stat()
	final, finalErr := os.Lstat(filename)
	if err != nil || statErr != nil || finalErr != nil || int64(len(data)) != before.Size() || !os.SameFile(before, after) || !os.SameFile(before, final) || after.Size() != before.Size() || after.ModTime() != before.ModTime() {
		return nil, "", fmt.Errorf("guest support source %s changed while reading", name)
	}
	return data, fmt.Sprintf("%x", sha256.Sum256(data)), nil
}
