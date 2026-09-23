package basebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/serialx"
)

type installerSerial interface {
	TartSlave() string
	InstallerWaitFor(context.Context, string) error
	InstallerSendLine(context.Context, string) error
	Close() error
}

type installerProcessSpec struct {
	path string
	args []string
	env  []string
	dir  string
}

type installerProcessStarter func(context.Context, installerProcessSpec) (backend.Handle, error)
type installerSerialFactory func(context.Context, string, string) (installerSerial, error)

// OSInstallerLauncher is the production generic-base installer launch policy.
// TartVM rechecks the pinned Tart executable before reaching this boundary.
type OSInstallerLauncher struct{}

func (OSInstallerLauncher) StartInstaller(ctx context.Context, request InstallerLaunch) (InstallerHandle, error) {
	return startInstaller(ctx, request, func(ctx context.Context, dir, runID string) (installerSerial, error) {
		return serialx.CreateInstallerRuntime(ctx, dir, runID)
	}, startInstallerProcess)
}

func startInstaller(ctx context.Context, launch InstallerLaunch, createSerial installerSerialFactory, start installerProcessStarter) (InstallerHandle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	request := launch.Request
	if err := backend.ValidateObjectID(request.CandidateID); err != nil {
		return nil, err
	}
	if !validRunID(request.RunID) {
		return nil, errors.New("installer run ID is invalid")
	}
	if !absoluteClean(request.ISOPath) || strings.Contains(request.ISOPath, ":") || filepath.Base(request.ISOPath) != "installer.iso" || !absoluteClean(request.SerialDirectory) || request.SerialDirectory != filepath.Join(filepath.Dir(request.ISOPath), "serial") {
		return nil, errors.New("installer paths do not match one private attempt")
	}
	for _, path := range []string{launch.TartPath, launch.TartHome, launch.SoftnetBinDir, launch.OperatorHome} {
		if !absoluteClean(path) {
			return nil, errors.New("installer launch path is not canonical and absolute")
		}
	}
	if filepath.Base(launch.TartPath) != "tart" || strings.Contains(launch.SoftnetBinDir, ":") || launch.OperatorName == "" || strings.IndexFunc(launch.OperatorName, func(c rune) bool { return unicode.IsControl(c) || c == '=' }) >= 0 || createSerial == nil || start == nil {
		return nil, errors.New("installer launch configuration is incomplete")
	}
	attemptDir := filepath.Dir(request.ISOPath)
	if err := privateStateRoot(attemptDir); err != nil {
		return nil, err
	}
	entry, err := os.Lstat(request.ISOPath)
	if err != nil {
		return nil, err
	}
	if err := privateRegular(request.ISOPath, entry); err != nil {
		return nil, err
	}
	if entry.Size() < 3<<30 || entry.Size() > 8<<30 {
		return nil, errors.New("installer ISO size is outside bound")
	}
	iso, err := os.OpenFile(request.ISOPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := iso.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		iso.Close()
		return nil, errors.New("installer ISO changed while opening")
	}
	serial, err := createSerial(ctx, attemptDir, request.RunID)
	if err != nil {
		if serial != nil {
			err = errors.Join(err, serial.Close())
		}
		iso.Close()
		return nil, fmt.Errorf("create private installer serial: %w", err)
	}
	if serial == nil {
		return nil, errors.Join(errors.New("installer serial returned no owned runtime"), iso.Close())
	}
	if serial.TartSlave() != filepath.Join(request.SerialDirectory, "tart-serial") {
		return nil, errors.Join(errors.New("installer serial returned an unexpected endpoint"), serial.Close(), iso.Close())
	}
	spec := installerProcessSpec{
		path: launch.TartPath,
		args: []string{"run", "--net-softnet", "--no-audio", "--no-clipboard", "--serial-path", serial.TartSlave(), "--disk=" + request.ISOPath + ":ro", request.CandidateID},
		env:  []string{"PATH=" + launch.SoftnetBinDir, "HOME=" + launch.OperatorHome, "USER=" + launch.OperatorName, "LOGNAME=" + launch.OperatorName, "TART_HOME=" + launch.TartHome, "TMPDIR=" + attemptDir, "LANG=C", "LC_ALL=C"},
		dir:  attemptDir,
	}
	process, startErr := start(ctx, spec)
	if process == nil {
		return nil, errors.Join(startErr, errors.New("installer start returned no owned process"), serial.Close(), iso.Close())
	}
	handle := &ownedInstallerHandle{process: process, serial: serial, iso: iso}
	if startErr != nil {
		return handle, fmt.Errorf("installer start after process creation: %w", startErr)
	}
	return handle, nil
}

func startInstallerProcess(ctx context.Context, spec installerProcessSpec) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer devNull.Close()
	process, err := os.StartProcess(spec.path, append([]string{spec.path}, spec.args...), &os.ProcAttr{
		Dir:   spec.dir,
		Env:   append([]string(nil), spec.env...),
		Files: []*os.File{devNull, devNull, devNull},
		Sys:   &syscall.SysProcAttr{Setpgid: true},
	})
	if err != nil {
		return nil, err
	}
	owned := &ownedScriptProcess{process: process, done: make(chan struct{}), signal: syscall.Kill, poll: pollScriptWait}
	go owned.reap()
	return &installerProcessHandle{owned: owned}, ctx.Err()
}

type installerProcessHandle struct{ owned *ownedScriptProcess }

func (h *installerProcessHandle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return h.owned.stop()
}

func (h *installerProcessHandle) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.owned.done:
		return h.owned.waitErr
	}
}

type ownedInstallerHandle struct {
	process   backend.Handle
	serial    installerSerial
	iso       *os.File
	closeOnce sync.Once
	closeErr  error
}

func (h *ownedInstallerHandle) WaitFor(ctx context.Context, marker string) error {
	return h.serial.InstallerWaitFor(ctx, marker)
}
func (h *ownedInstallerHandle) SendLine(ctx context.Context, line string) error {
	return h.serial.InstallerSendLine(ctx, line)
}
func (h *ownedInstallerHandle) Stop(ctx context.Context) error { return h.process.Stop(ctx) }

func (h *ownedInstallerHandle) Wait(ctx context.Context) error {
	err := h.process.Wait(ctx)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrScriptReapUnproven) {
		return err // Preserve serial and ISO while child ownership is uncertain.
	}
	h.closeOnce.Do(func() { h.closeErr = errors.Join(h.serial.Close(), h.iso.Close()) })
	return errors.Join(err, h.closeErr)
}

var _ InstallerLauncher = OSInstallerLauncher{}
var _ InstallerHandle = (*ownedInstallerHandle)(nil)
