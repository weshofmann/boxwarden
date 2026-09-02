package hostx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/execx"
)

const maxInstallRequestBytes = 16 << 10

// InstallRequest is the sole versioned parent-to-root message. It contains no
// passwords, key bytes, environment, or caller-supplied destination path.
type InstallRequest struct {
	Version       int          `json:"version"`
	SoftnetSource string       `json:"softnet_source"`
	Tart          ToolIdentity `json:"tart"`
	TartHome      string       `json:"tart_home"`
}

func (r InstallRequest) Validate() error {
	if r.Version != 1 || !canonicalAbsolute(r.SoftnetSource) || !qualifiedTart(r.Tart) || !canonicalAbsolute(r.Tart.Path) {
		return fmt.Errorf("invalid qualified install request")
	}
	if !canonicalAbsolute(r.TartHome) {
		return fmt.Errorf("invalid trusted path request")
	}
	return nil
}

// Caller is derived by the root process from sudo's trusted environment and a
// platform user lookup. It is never accepted from the unprivileged request.
type Caller struct {
	UID        int
	Name, Home string
}
type RootIdentity interface {
	EffectiveUID() int
	SudoCaller() (Caller, error)
}
type GroupManager interface {
	Ensure(Caller, string) (Group, bool, error)
}
type FilesystemPublisher interface {
	Preflight(context.Context, InstallRequest, Caller) (publicationState, error)
	State(context.Context, InstallRequest, Caller, Group) (publicationState, error)
	Publish(context.Context, InstallRequest, Caller, Group) error
}
type publicationState uint8

const (
	publicationAbsent publicationState = iota
	publicationComplete
	publicationUnexpected
)

type RootInstallResult struct {
	Published           bool `json:"published"`
	AlreadyInstalled    bool `json:"already_installed"`
	RefreshLoginSession bool `json:"refresh_login_session"`
}
type RootInstaller struct {
	Identity  RootIdentity
	Groups    GroupManager
	Publisher FilesystemPublisher
}

func (i RootInstaller) Install(ctx context.Context, request InstallRequest) (RootInstallResult, error) {
	if err := request.Validate(); err != nil {
		return RootInstallResult{}, err
	}
	if i.Identity == nil || i.Groups == nil || i.Publisher == nil {
		return RootInstallResult{}, fmt.Errorf("root installation adapters are required")
	}
	if i.Identity.EffectiveUID() != 0 {
		return RootInstallResult{}, fmt.Errorf("host-install requires effective UID 0")
	}
	caller, err := i.Identity.SudoCaller()
	if err != nil {
		return RootInstallResult{}, fmt.Errorf("derive sudo caller: %w", err)
	}
	if caller.UID <= 0 || caller.Name == "" || !canonicalAbsolute(caller.Home) {
		return RootInstallResult{}, fmt.Errorf("invalid sudo caller identity")
	}
	preflight, err := i.Publisher.Preflight(ctx, request, caller)
	if err != nil {
		return RootInstallResult{}, fmt.Errorf("inspect install tree before directory-service mutation: %w", err)
	}
	if preflight == publicationUnexpected {
		return RootInstallResult{}, fmt.Errorf("refuse unexpected or partial existing digest tree; inspect manually")
	}
	group, changed, err := i.Groups.Ensure(caller, OperatorGroupName)
	if err != nil {
		return RootInstallResult{}, fmt.Errorf("ensure operator group: %w", err)
	}
	if group.Name != OperatorGroupName || group.ID < 0 || len(group.Members) != 1 || group.Members[0] != caller.UID {
		return RootInstallResult{}, fmt.Errorf("operator group does not exactly bind sudo caller")
	}
	state, err := i.Publisher.State(ctx, request, caller, group)
	if err != nil {
		return RootInstallResult{}, fmt.Errorf("inspect install tree: %w", err)
	}
	switch state {
	case publicationComplete:
		return RootInstallResult{AlreadyInstalled: true, RefreshLoginSession: changed}, nil
	case publicationUnexpected:
		return RootInstallResult{}, fmt.Errorf("refuse unexpected or partial existing digest tree; inspect manually")
	case publicationAbsent:
		if err := i.Publisher.Publish(ctx, request, caller, group); err != nil {
			return RootInstallResult{}, fmt.Errorf("publish digest tree: %w", err)
		}
		return RootInstallResult{Published: true, RefreshLoginSession: changed}, nil
	default:
		return RootInstallResult{}, fmt.Errorf("unknown publication state")
	}
}

func EncodeInstallRequest(request InstallRequest) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode install request: %w", err)
	}
	if len(data) > maxInstallRequestBytes {
		return nil, fmt.Errorf("install request exceeds bounded input")
	}
	return data, nil
}

func DecodeInstallRequest(data []byte) (InstallRequest, error) {
	if len(data) == 0 || len(data) > maxInstallRequestBytes {
		return InstallRequest{}, fmt.Errorf("install request exceeds bounded input")
	}
	if err := rejectDuplicateFields(data); err != nil {
		return InstallRequest{}, fmt.Errorf("decode install request: %w", err)
	}
	var request InstallRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return InstallRequest{}, fmt.Errorf("decode install request: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return InstallRequest{}, fmt.Errorf("decode install request: trailing JSON value")
		}
		return InstallRequest{}, fmt.Errorf("decode install request tail: %w", err)
	}
	if err := request.Validate(); err != nil {
		return InstallRequest{}, err
	}
	return request, nil
}

func ReadRootInstallRequest(reader io.Reader) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("root host-install stdin is required")
	}
	data, err := io.ReadAll(io.LimitReader(reader, maxInstallRequestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read root host-install request: %w", err)
	}
	if len(data) == 0 || len(data) > maxInstallRequestBytes {
		return nil, fmt.Errorf("root host-install request exceeds bounded input")
	}
	return data, nil
}

// InvokeRootInstall composes the only allowed privilege transition. The caller
// validates bytes before this point; stdin remains bounded by execx.
func InvokeRootInstall(ctx context.Context, runner PrivilegeRunner, executable string, request InstallRequest) (RootInstallResult, error) {
	if runner == nil || !canonicalAbsolute(executable) {
		return RootInstallResult{}, fmt.Errorf("qualified absolute executable and privilege runner are required")
	}
	data, err := EncodeInstallRequest(request)
	if err != nil {
		return RootInstallResult{}, err
	}
	result, err := runner.Run(ctx, execx.Command{Path: "/usr/bin/sudo", Args: []string{"--", executable, "internal", "host-install"}, Env: []string{}, Stdin: data})
	if err != nil {
		return RootInstallResult{}, fmt.Errorf("run root host-install phase: %w", err)
	}
	if result.Truncated {
		return RootInstallResult{}, fmt.Errorf("root host-install result was truncated")
	}
	decoded, err := DecodeRootInstallResult([]byte(result.Stdout))
	if err != nil {
		return RootInstallResult{}, err
	}
	return decoded, nil
}

func EncodeRootInstallResult(result RootInstallResult) ([]byte, error) {
	if result.Published == result.AlreadyInstalled {
		return nil, fmt.Errorf("root result must be exactly published or already installed")
	}
	return json.Marshal(result)
}

func DecodeRootInstallResult(data []byte) (RootInstallResult, error) {
	if len(data) == 0 || len(data) > maxInstallRequestBytes {
		return RootInstallResult{}, fmt.Errorf("root host-install result exceeds bounded input")
	}
	if err := rejectDuplicateFields(data); err != nil {
		return RootInstallResult{}, fmt.Errorf("decode root host-install result: %w", err)
	}
	var result RootInstallResult
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return RootInstallResult{}, fmt.Errorf("decode root host-install result: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return RootInstallResult{}, fmt.Errorf("decode root host-install result: trailing content")
	}
	if result.Published == result.AlreadyInstalled {
		return RootInstallResult{}, fmt.Errorf("root result must be exactly published or already installed")
	}
	return result, nil
}

// ValidateUnprivilegedSource rejects every form of source indirection or
// privilege bit before its digest is admitted to the root phase.
func ValidateUnprivilegedSource(path string, expectedDigest string) error {
	if !canonicalAbsolute(path) || expectedDigest != SoftnetExecutableSHA256 {
		return fmt.Errorf("unqualified Softnet source")
	}
	file, err := openVerifiedRegular(path, expectedDigest, true)
	if err != nil {
		return fmt.Errorf("inspect Softnet source: %w", err)
	}
	return file.Close()
}
