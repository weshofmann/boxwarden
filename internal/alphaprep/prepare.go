package alphaprep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/alphaqual"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/diskreserve"
	"github.com/weshofmann/boxwarden/internal/privateacl"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// Prepare composes one automatic alpha base preparation attempt. Host and CA
// admission, tool digests, exact source intent, and the full authority graph
// are checked before the first attempt-root mutation. The reusable cache is
// preparation evidence, not certification of the guest or final alpha.
func Prepare(ctx context.Context, loaded config.Config, selected config.Domain, configPath string, request basebuild.PrepareRequest, doctor HostRuntimeChecker, ca DomainCAChecker, components BuildComponents) (basebuild.PreparedResult, error) {
	attemptRoot := filepath.Join(selected.StateRoot, "prepared-attempts")
	if request.StateRoot != selected.StateRoot || request.Inputs.AttemptRoot != attemptRoot {
		return basebuild.PreparedResult{}, errors.New("preparation paths do not belong to exact selected domain")
	}
	runtime, err := Preflight(ctx, loaded, selected, doctor, ca)
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	build, err := NewBuildDependencies(loaded, selected, runtime, components)
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	key, err := build.Checks.Verify(request.Inputs)
	if err != nil {
		return basebuild.PreparedResult{}, fmt.Errorf("verify prepared-base source: %w", err)
	}
	reader, err := supervisor.NewExactSnapshotReader(filepath.Join(selected.StateRoot, "runtime"))
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	inspector, err := alphaqual.NewPreparedBaseInspector(request.Inputs.Recipe, key, reader, time.Now)
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	vm, ok := components.Observer.(qualificationBackend)
	if !ok {
		return basebuild.PreparedResult{}, errors.New("one backend must own candidate observation and fresh clone creation")
	}
	qualifier, err := NewProductionQualifier(loaded, selected, configPath, vm, inspector)
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	var result basebuild.PreparedResult
	err = diskreserve.Run(ctx, []string{selected.StateRoot, runtime.Manifest.TartHome}, func(guarded context.Context) error {
		if err := ensureAttemptRoot(selected.StateRoot); err != nil {
			return err
		}
		var prepareErr error
		result, prepareErr = basebuild.Prepare(guarded, request, basebuild.PrepareDependencies{Build: build, Qualifier: qualifier})
		return prepareErr
	})
	if err != nil {
		return basebuild.PreparedResult{}, err
	}
	return result, nil
}

func ensureAttemptRoot(stateRoot string) error {
	root, err := os.OpenRoot(stateRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	const name = "prepared-attempts"
	if err := root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create private attempt root: %w", err)
	}
	info, err := root.Lstat(name)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return errors.New("prepared attempt root has unsafe metadata")
	}
	path := filepath.Join(stateRoot, name)
	if err := privateacl.Check(path, info, privateacl.OSInspector{}); err != nil {
		return err
	}
	opened, err := root.Open(name)
	if err != nil {
		return err
	}
	defer opened.Close()
	current, err := opened.Stat()
	if err != nil || !os.SameFile(info, current) {
		return errors.New("prepared attempt root changed during admission")
	}
	return nil
}
