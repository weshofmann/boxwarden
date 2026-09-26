//go:build darwin

package workspaceformat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
)

// Opt-in host check. The paths are private operator inputs, never source
// fixtures; this admits the signed bundle and does not start a VM or format.
func TestVZFormatterRealPreparedBundleAdmission(t *testing.T) {
	configPath := os.Getenv("BOXWARDEN_ALPHA_FORMATTER_CONFIG")
	bundle := os.Getenv("BOXWARDEN_ALPHA_FORMATTER_BUNDLE")
	if configPath == "" && bundle == "" {
		t.Skip("private formatter config and bundle are not supplied")
	}
	if configPath == "" || bundle == "" {
		t.Fatal("private formatter config and bundle must be supplied together")
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := loaded.Domain("alpha")
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	formatter := VZFormatter{StateRoot: selected.StateRoot, Domain: selected.ID, BundlePath: bundle, SourceRoot: sourceRoot}
	if err := formatter.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}
