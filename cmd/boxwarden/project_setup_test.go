package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/projectx"
)

func TestProjectSetupRejectsMissingInputsWithoutChangingState(t *testing.T) {
	for _, missing := range []string{"source", "Go", "ISO"} {
		t.Run(missing, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state")
			source := filepath.Join(root, "source")
			if err := os.Mkdir(state, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			setup := projectx.Setup{Version: 1, SourceRoot: source, FormatterBundle: filepath.Join(root, "formatter"),
				ISOPath: filepath.Join(root, "ubuntu.iso"), GoBinary: filepath.Join(root, "go")}
			if err := os.WriteFile(setup.GoBinary, []byte("fixture executable"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(setup.ISOPath, []byte("wrong installer"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch missing {
			case "source":
				setup.SourceRoot = filepath.Join(root, "missing-source")
			case "Go":
				setup.GoBinary = filepath.Join(root, "missing-go")
			case "ISO":
				setup.ISOPath = filepath.Join(root, "missing.iso")
			}
			err := checkProjectSetup(t.Context(), config.Domain{ID: domain.ID("alpha"), StateRoot: state}, setup)
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Fatalf("missing %s dependency error = %v", missing, err)
			}
			entries, readErr := os.ReadDir(state)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("setup admission mutated state: entries=%v err=%v", entries, readErr)
			}
		})
	}
}

func TestProjectSetupRejectsUnpinnedISO(t *testing.T) {
	root := t.TempDir()
	setup := projectx.Setup{Version: 1, SourceRoot: root, FormatterBundle: filepath.Join(root, "formatter"),
		ISOPath: filepath.Join(root, "ubuntu.iso"), GoBinary: filepath.Join(root, "go")}
	if err := os.WriteFile(setup.GoBinary, []byte("fixture executable"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(setup.ISOPath, []byte("untrusted installer"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := checkProjectSetup(t.Context(), config.Domain{ID: domain.ID("alpha"), StateRoot: root}, setup)
	if err == nil || !strings.Contains(err.Error(), "ISO") || !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("unpinned ISO admission error = %v", err)
	}
}

func TestProjectGoExecutableRequiresActualSafeFile(t *testing.T) {
	for _, kind := range []string{"executable", "missing", "not executable", "symlink", "directory", "writable"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(root, "go")
			mode := os.FileMode(0o755)
			if kind == "not executable" {
				mode = 0o644
			} else if kind == "writable" {
				mode = 0o777
			}
			switch kind {
			case "missing":
			case "directory":
				if err := os.Mkdir(binary, 0o700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.WriteFile(filepath.Join(root, "actual"), []byte("fixture"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, "actual"), binary); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(binary, []byte("fixture"), mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(binary, mode); err != nil {
					t.Fatal(err)
				}
			}
			err := checkProjectGoBinary(binary)
			if (err == nil) != (kind == "executable") {
				t.Fatalf("Go %s admission error = %v", kind, err)
			}
		})
	}
}

func TestProjectGoExecutableRejectsRelativeAndControlPaths(t *testing.T) {
	for _, path := range []string{"go", "/tmp/../go", "/tmp/go\n"} {
		if err := checkProjectGoBinary(path); err == nil {
			t.Fatalf("accepted unsafe Go path %q", path)
		}
	}
}

func TestProjectSetupHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := checkProjectSetup(ctx, config.Domain{}, projectx.Setup{})
	if err != context.Canceled {
		t.Fatalf("canceled setup admission error = %v", err)
	}
}

func TestPrebuiltProjectSetupDoesNotRequireGo(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	os.Mkdir(source, 0700)
	setup := projectx.Setup{Version: 3, SourceRoot: source, FormatterBundle: filepath.Join(root, "formatter"), ISOPath: filepath.Join(root, "missing.iso"), PrebuiltResources: filepath.Join(root, "missing.resources")}
	err := checkProjectSetup(context.Background(), config.Domain{ID: "work", StateRoot: filepath.Join(root, "state")}, setup)
	if err == nil || strings.Contains(err.Error(), "--go") {
		t.Fatalf("prebuilt setup demanded compiler: %v", err)
	}
}
