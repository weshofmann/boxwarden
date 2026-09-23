package basebuild

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

// InstallerLauncher is the installer ownership boundary. It creates one
// private PTY, retains the exact Tart child/group through stop and reap,
// bounds serial read/write, and attaches only Request.ISOPath as read-only.
type InstallerLauncher interface {
	StartInstaller(context.Context, InstallerLaunch) (InstallerHandle, error)
}

type InstallerLaunch struct {
	Request       InstallerRequest
	TartPath      string
	TartHome      string
	SoftnetBinDir string
	OperatorHome  string
	OperatorName  string
}

type TartVMConfig struct {
	Runner        execx.Runner
	Observer      backend.Observer
	TartPath      string
	TartSHA256    string
	TartHome      string
	SoftnetBinDir string
	OperatorHome  string
	OperatorName  string
	Launcher      InstallerLauncher
}

// TartVM adapts observation and bounded stopped-object mutation to the pinned
// Tart CLI. Constructor admission requires an owned installer launcher so a
// caller cannot create a candidate that this adapter is incapable of booting.
type TartVM struct{ config TartVMConfig }

func NewTartVM(config TartVMConfig) (*TartVM, error) {
	if config.Runner == nil || config.Observer == nil || config.Launcher == nil {
		return nil, errors.New("Tart runner, qualified observer, and owned installer launcher are required")
	}
	if err := exactExecutable(config.TartPath, "tart", config.TartSHA256); err != nil {
		return nil, fmt.Errorf("qualified Tart: %w", err)
	}
	for _, path := range []string{config.TartHome, config.SoftnetBinDir, config.OperatorHome} {
		if !absoluteClean(path) {
			return nil, errors.New("Tart launch paths must be canonical and absolute")
		}
	}
	if config.OperatorName == "" || strings.IndexFunc(config.OperatorName, func(c rune) bool { return unicode.IsControl(c) || c == '=' }) >= 0 {
		return nil, errors.New("Tart operator name is invalid")
	}
	return &TartVM{config: config}, nil
}

func (v *TartVM) Observe(ctx context.Context, id string) (backend.Observation, error) {
	if v == nil {
		return backend.Observation{}, errors.New("Tart VM adapter is nil")
	}
	if err := backend.ValidateObjectID(id); err != nil {
		return backend.Observation{}, err
	}
	if err := exactExecutable(v.config.TartPath, "tart", v.config.TartSHA256); err != nil {
		return backend.Observation{}, err
	}
	return v.config.Observer.Observe(ctx, id)
}

func (v *TartVM) Create(ctx context.Context, request CreateRequest) error {
	if v == nil {
		return errors.New("Tart VM adapter is nil")
	}
	if err := backend.ValidateObjectID(request.CandidateID); err != nil {
		return err
	}
	if request.DiskGiB < 20 || request.DiskGiB > 2048 {
		return errors.New("candidate disk size is outside recipe bounds")
	}
	return v.run(ctx, 10*time.Minute, []string{"create", request.CandidateID, "--linux", "--disk-size", strconv.Itoa(request.DiskGiB)})
}

func (v *TartVM) Configure(ctx context.Context, request ConfigureRequest) error {
	if v == nil {
		return errors.New("Tart VM adapter is nil")
	}
	if err := backend.ValidateObjectID(request.CandidateID); err != nil {
		return err
	}
	if request.CPUs < 1 || request.CPUs > 16 || request.MemoryMiB < 2048 || request.MemoryMiB > 65536 {
		return errors.New("candidate resources are outside recipe bounds")
	}
	return v.run(ctx, time.Minute, []string{"set", request.CandidateID, "--cpu", strconv.Itoa(request.CPUs), "--memory", strconv.Itoa(request.MemoryMiB)})
}

func (v *TartVM) run(ctx context.Context, limit time.Duration, args []string) error {
	if err := exactExecutable(v.config.TartPath, "tart", v.config.TartSHA256); err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	result, err := v.config.Runner.Run(runCtx, execx.Command{Path: v.config.TartPath, Args: args, Env: []string{"PATH=/usr/bin:/bin", "TART_HOME=" + v.config.TartHome, "LANG=C", "LC_ALL=C"}})
	if err != nil {
		return fmt.Errorf("fixed Tart %s: %w", args[0], err)
	}
	if result.Truncated {
		return fmt.Errorf("fixed Tart %s output exceeded limit", args[0])
	}
	return nil
}

func (v *TartVM) RunInstaller(ctx context.Context, request InstallerRequest) (InstallerHandle, error) {
	if v == nil {
		return nil, errors.New("Tart VM adapter is nil")
	}
	if err := backend.ValidateObjectID(request.CandidateID); err != nil {
		return nil, err
	}
	if !validRunID(request.RunID) {
		return nil, errors.New("installer run ID is invalid")
	}
	if !absoluteClean(request.ISOPath) || filepath.Base(request.ISOPath) != "installer.iso" || !absoluteClean(request.SerialDirectory) || request.SerialDirectory != filepath.Join(filepath.Dir(request.ISOPath), "serial") {
		return nil, errors.New("installer ISO or serial path is outside the exact attempt layout")
	}
	if err := privateStateRoot(filepath.Dir(request.ISOPath)); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(request.SerialDirectory); !errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("installer serial directory must not exist before launch")
	}
	entry, err := os.Lstat(request.ISOPath)
	if err != nil {
		return nil, err
	}
	if err := privateRegular(request.ISOPath, entry); err != nil {
		return nil, err
	}
	if entry.Size() < 3<<30 || entry.Size() > 8<<30 {
		return nil, errors.New("installer ISO size is outside the supported source range")
	}
	file, err := os.OpenFile(request.ISOPath, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	closeErr := file.Close()
	if err != nil || closeErr != nil || !os.SameFile(entry, opened) {
		return nil, errors.New("installer ISO changed while opening")
	}
	if err := exactExecutable(v.config.TartPath, "tart", v.config.TartSHA256); err != nil {
		return nil, err
	}
	handle, err := v.config.Launcher.StartInstaller(ctx, InstallerLaunch{Request: request, TartPath: v.config.TartPath, TartHome: v.config.TartHome, SoftnetBinDir: v.config.SoftnetBinDir, OperatorHome: v.config.OperatorHome, OperatorName: v.config.OperatorName})
	if handle == nil {
		return nil, errors.Join(errors.New("installer launcher returned no owned handle"), err)
	}
	return handle, err
}

var _ VirtualMachine = (*TartVM)(nil)
