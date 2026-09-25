package basebuild

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

// This fixed set mirrors recipe.GuestDefinitionDigest. If that definition
// grows, verification of this staged tree fails closed until staging grows.
var stagedGuestDefinitionFiles = []string{
	"artifacts.lock.json",
	"artifacts/boxwarden-guest-bootstrap",
	"autoinstall/meta-data",
	"autoinstall/user-data",
	"finalize-golden.sh",
	"install-pinned-chatgpt.py",
	"recipe-prepare.py",
	"remaster-golden-iso.sh",
	"render-golden-seed.sh",
}

func stageBuildInputs(ctx context.Context, in Inputs, attemptDir string) (Inputs, error) {
	if !absoluteClean(in.ISOPath) || !absoluteClean(in.GuestDefinitionRoot) || !absoluteClean(attemptDir) {
		return Inputs{}, errors.New("source and attempt paths must be canonical and absolute")
	}
	if err := privateStateRoot(attemptDir); err != nil {
		return Inputs{}, err
	}
	guest := filepath.Join(attemptDir, "guest-definition")
	if err := os.Mkdir(guest, 0700); err != nil {
		return Inputs{}, fmt.Errorf("reserve staged guest definition: %w", err)
	}
	for _, dir := range []string{"artifacts", "autoinstall"} {
		if err := os.Mkdir(filepath.Join(guest, dir), 0700); err != nil {
			return Inputs{}, err
		}
	}
	for _, name := range stagedGuestDefinitionFiles {
		mode := os.FileMode(0400)
		switch name {
		case "artifacts/boxwarden-guest-bootstrap", "finalize-golden.sh", "install-pinned-chatgpt.py", "recipe-prepare.py", "remaster-golden-iso.sh", "render-golden-seed.sh":
			mode = 0500
		}
		if err := stageSourceFile(ctx, filepath.Join(in.GuestDefinitionRoot, name), filepath.Join(guest, name), mode, 64<<20, false); err != nil {
			return Inputs{}, fmt.Errorf("stage guest definition %s: %w", name, err)
		}
	}
	iso := filepath.Join(attemptDir, "source.iso")
	if err := stageSourceFile(ctx, in.ISOPath, iso, 0400, 4<<30, true); err != nil {
		return Inputs{}, fmt.Errorf("stage pinned installer: %w", err)
	}
	in.ISOPath = iso
	in.GuestDefinitionRoot = guest
	return in, nil
}

func stageSourceFile(ctx context.Context, source, target string, mode os.FileMode, maximum int64, preferClone bool) error {
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil || resolved != source {
		return fmt.Errorf("source has a symlinked path: %v", err)
	}
	entry, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !entry.Mode().IsRegular() || entry.Size() <= 0 || entry.Size() > maximum {
		return errors.New("source type or size is outside bounds")
	}
	input, err := os.OpenFile(source, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer input.Close()
	opened, err := input.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return errors.New("source changed while opening")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if preferClone {
		cloned, err := cloneStagedFile(input, target, mode)
		if err != nil {
			return err
		}
		if cloned {
			if err := ctx.Err(); err != nil {
				return err
			}
			after, err := input.Stat()
			if err != nil || !os.SameFile(entry, after) || after.Size() != entry.Size() || after.ModTime() != entry.ModTime() {
				return errors.New("source changed while cloning")
			}
			return nil
		}
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, mode)
	if err != nil {
		return err
	}
	var copied int64
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			_ = output.Close()
			return err
		}
		n, readErr := input.Read(buf)
		if n > 0 {
			copied += int64(n)
			if copied > maximum {
				_ = output.Close()
				return errors.New("source grew beyond staging limit")
			}
			if _, err := output.Write(buf[:n]); err != nil {
				_ = output.Close()
				return err
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = output.Close()
			return readErr
		}
	}
	if copied != entry.Size() {
		_ = output.Close()
		return errors.New("source length changed while staging")
	}
	after, err := input.Stat()
	if err != nil || !os.SameFile(entry, after) || after.Size() != entry.Size() || after.ModTime() != entry.ModTime() {
		_ = output.Close()
		return errors.New("source changed while staging")
	}
	return errors.Join(output.Sync(), output.Close())
}
