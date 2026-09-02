package hostx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func (s SystemService) Init(ctx context.Context, request Request) (InitResult, error) {
	inspector := s.inspector
	if inspector == nil {
		inspector = NewOSDoctorInspector()
	}
	platform := inspector.Platform()
	if platform.OS != QualifiedPlatform || platform.Arch != QualifiedArch || platform.Release != QualifiedMacOS {
		return InitResult{}, ErrUnsupportedPlatform
	}
	if request.Domain == "" || !canonicalAbsolute(request.StateRoot) || !canonicalAbsolute(request.TartPath) || !canonicalAbsolute(request.TartHome) || !canonicalAbsolute(request.SoftnetPath) {
		return InitResult{}, fmt.Errorf("init requires validated domain and canonical absolute host paths")
	}
	if pathsOverlap(request.StateRoot, request.TartPath) || pathsOverlap(request.StateRoot, request.SoftnetPath) || pathsOverlap(request.StateRoot, request.TartHome) || pathsOverlap(request.StateRoot, productionToolchainPath()) || pathsOverlap(request.TartHome, request.TartPath) || pathsOverlap(request.TartHome, request.SoftnetPath) || pathsOverlap(request.TartPath, request.SoftnetPath) {
		return InitResult{}, fmt.Errorf("host prerequisite path overlaps domain state root")
	}
	unsafe, err := inspector.HomebrewSoftnet()
	if err != nil {
		return InitResult{}, fmt.Errorf("inspect mutable Homebrew Softnet privilege state")
	}
	for _, target := range unsafe {
		if target.Privilege != "" {
			return InitResult{}, fmt.Errorf("refuse privileged mutable Homebrew Softnet at %s; inspect and remediate manually", target.Path)
		}
	}
	tart, err := inspector.InspectPath(request.TartPath)
	if err != nil || !tart.Exists || !tart.Regular || tart.Links != 1 || tart.ExtendedACL || tart.SHA256 != TartExecutableSHA256 {
		return InitResult{}, fmt.Errorf("configured Tart executable does not match qualified identity")
	}
	if output, err := inspector.CommandOutput(request.TartPath, "--version"); err != nil || !strings.Contains(output, TartVersion) {
		return InitResult{}, fmt.Errorf("configured Tart version does not match qualified identity")
	}
	tartHome, err := inspector.InspectPath(request.TartHome)
	if err != nil || !tartHome.Exists || !tartHome.Directory || tartHome.Mode&0o077 != 0 || tartHome.ExtendedACL {
		return InitResult{}, fmt.Errorf("tart_home must be an existing private direct directory")
	}
	validateSource := s.sourceValidator
	if validateSource == nil {
		validateSource = ValidateUnprivilegedSource
	}
	if err := validateSource(request.SoftnetPath, SoftnetExecutableSHA256); err != nil {
		return InitResult{}, err
	}
	executable := s.executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return InitResult{}, fmt.Errorf("resolve boxwarden executable: %w", err)
		}
		executable, err = filepath.EvalSymlinks(executable)
		if err != nil || !canonicalAbsolute(executable) {
			return InitResult{}, fmt.Errorf("resolve canonical boxwarden executable")
		}
	}
	if s.privilege == nil {
		return InitResult{}, fmt.Errorf("privilege runner is required")
	}
	rootResult, err := InvokeRootInstall(ctx, s.privilege, executable, InstallRequest{
		Version: 1, SoftnetSource: request.SoftnetPath,
		Tart:     ToolIdentity{Path: request.TartPath, Version: TartVersion, ExecutableSHA256: TartExecutableSHA256, ArchiveSHA256: TartArchiveSHA256},
		TartHome: request.TartHome,
	})
	if err != nil {
		return InitResult{}, err
	}
	return InitResult{HostInstalled: rootResult.Published || rootResult.AlreadyInstalled, RefreshLoginSession: rootResult.RefreshLoginSession}, nil
}
