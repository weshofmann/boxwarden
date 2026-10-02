package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

// checkProjectSetup admits remembered operator inputs without creating any
// runtime state. The formatter repeats its admission when a disk is created;
// export independently admits its own inspector inputs.
func checkProjectSetup(ctx context.Context, selected config.Domain, setup projectx.Setup) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, input := range []struct{ flag, path string }{
		{"source-root", setup.SourceRoot}, {"formatter-bundle", setup.FormatterBundle},
		{"iso", setup.ISOPath}, {"go", setup.GoBinary},
	} {
		if !canonicalProjectSetupPath(input.path) {
			return fmt.Errorf("project setup --%s requires a clean absolute path", input.flag)
		}
	}
	source, err := os.Stat(setup.SourceRoot)
	if err != nil {
		return fmt.Errorf("project setup source checkout %q is unavailable; supply --source-root with a clean committed checkout: %w", setup.SourceRoot, err)
	}
	if !source.IsDir() {
		return errors.New("project setup source checkout must be a directory; supply --source-root with a clean committed checkout")
	}
	if err := checkProjectGoBinary(setup.GoBinary); err != nil {
		return err
	}
	if err := recipe.VerifyISO(setup.ISOPath); err != nil {
		return fmt.Errorf("project setup ISO %q is not the admitted pinned installer; supply --iso with Ubuntu 24.04.4 Desktop ARM64: %w", setup.ISOPath, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	formatter := workspaceformat.VZFormatter{StateRoot: selected.StateRoot, Domain: selected.ID,
		BundlePath: setup.FormatterBundle, SourceRoot: setup.SourceRoot}
	if err := formatter.Check(ctx); err != nil {
		return fmt.Errorf("project setup formatter admission failed; supply --formatter-bundle and its matching clean --source-root for this domain: %w", err)
	}
	return nil
}

func checkProjectGoBinary(path string) error {
	if !canonicalProjectSetupPath(path) || filepath.Base(path) != "go" {
		return errors.New("project setup Go executable requires --go with the clean absolute path to the actual go file")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("project setup Go executable %q is unavailable; supply --go with the actual executable: %w", path, err)
	}
	valid := func(info os.FileInfo) bool {
		return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 && info.Mode().Perm()&0o022 == 0
	}
	if !valid(before) {
		return errors.New("project setup Go file must be regular, executable, non-symlink, and not writable by group or others; supply --go with the actual executable")
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("project setup open Go executable: %w", err)
	}
	opened, statErr := file.Stat()
	closeErr := file.Close()
	if err := errors.Join(statErr, closeErr); err != nil {
		return fmt.Errorf("project setup inspect Go executable: %w", err)
	}
	if !valid(opened) || !os.SameFile(before, opened) {
		return errors.New("project setup Go executable changed while opening; inspect --go and retry")
	}
	return nil
}

func canonicalProjectSetupPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && strings.IndexFunc(path, unicode.IsControl) < 0
}
