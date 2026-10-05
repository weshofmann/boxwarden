package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/projectx"
)

// SetupInspection describes only alpha project prerequisites. It grants no
// runtime authority and does not attest a prepared base or VM READY.
type SetupInspection struct {
	Version                    int      `json:"version"`
	Scope                      string   `json:"scope"`
	Status                     string   `json:"status"`
	ConfigPath                 string   `json:"config_path"`
	ConfigValid                bool     `json:"config_valid"`
	SelectionAcceptable        bool     `json:"selection_acceptable"`
	Guidance                   string   `json:"guidance"`
	NextActions                []string `json:"next_actions"`
	SetupVersion               int      `json:"setup_version,omitempty"`
	RecipePreparationAvailable bool     `json:"recipe_preparation_available"`
	Diagnostic                 string   `json:"diagnostic,omitempty"`
}

// Handle before normal configuration loading: missing/invalid configuration is
// a completed inspection result, not a failed command without structured output.
func runSetupInspect(ctx context.Context, args []string, o Options) (bool, error) {
	set := flag.NewFlagSet("boxwarden", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	path := set.String("config", o.ConfigPath, "configuration file")
	domain := set.String("domain", "", "security domain")
	if err := set.Parse(args); err != nil {
		return false, nil
	}
	rest := set.Args()
	if len(rest) == 0 || rest[0] != "setup" {
		return false, nil
	}
	explicitDomain := false
	explicitConfig := false
	set.Visit(func(f *flag.Flag) {
		if f.Name == "domain" {
			explicitDomain = true
		}
		if f.Name == "config" {
			explicitConfig = true
		}
	})
	if explicitDomain || *domain != "" {
		return true, errors.New("setup inspect is alpha-scoped and does not accept --domain")
	}
	if len(rest) != 3 || rest[1] != "inspect" || rest[2] != "--json" {
		return true, errors.New("usage: boxwarden [--config PATH] setup inspect --json")
	}
	if o.Output == nil {
		return true, errors.New("command output is required")
	}
	if *path == "" && !explicitConfig {
		var err error
		*path, err = DefaultConfigPath()
		if err != nil {
			return true, err
		}
	}
	result := InspectSetup(ctx, *path, o)
	if err := ctx.Err(); err != nil {
		return true, err
	}
	return true, json.NewEncoder(o.Output).Encode(result)
}

func InspectSetup(ctx context.Context, path string, o Options) SetupInspection {
	r := SetupInspection{Version: 1, Scope: "alpha_project_setup", ConfigPath: path, NextActions: []string{}}
	fail := func(status, guidance string, actions []string, err error) SetupInspection {
		r.Status, r.Guidance, r.NextActions = status, guidance, actions
		if err != nil {
			r.Diagnostic = err.Error()
			if len(r.Diagnostic) > 4096 {
				r.Diagnostic = r.Diagnostic[:4096]
			}
		}
		return r
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexFunc(path, unicode.IsControl) >= 0 {
		return fail("config_location_inadmissible", "Select a configuration at a clean absolute path.", []string{"select_config"}, nil)
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return fail("config_missing", "Select an existing configuration or complete attended setup.", []string{"select_config", "attended_setup"}, err)
	}
	if err != nil {
		return fail("config_location_inadmissible", "The selected configuration cannot be inspected.", []string{"select_config"}, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fail("config_location_inadmissible", "Select a regular direct configuration file.", []string{"select_config"}, nil)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return fail("config_location_inadmissible", "Select the direct configuration file without symlink ancestors.", []string{"select_config"}, err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		switch {
		case errors.Is(err, config.ErrWorkspaceStorageUnavailable):
			return fail("workspace_storage_unavailable", "Reconnect the enrolled workspace volume and inspect its exact identity.", []string{"inspect_storage", "retry_inspection"}, err)
		case errors.Is(err, config.ErrHostPrerequisites):
			status := "host_tools_incompatible"
			if errors.Is(err, os.ErrNotExist) {
				status = "host_tools_uninitialized"
			}
			return fail(status, "Configured host tool paths failed admission; inspect the configuration and complete attended host setup.", []string{"inspect_config", "attended_host_setup"}, err)
		default:
			return fail("config_invalid", "The selected configuration failed validation; inspect it or select another configuration.", []string{"inspect_config", "select_config"}, err)
		}
	}
	r.ConfigValid = true
	d, err := loaded.Domain("alpha")
	if err != nil {
		return fail("config_invalid", "Select a configuration containing the alpha domain.", []string{"select_config"}, err)
	}
	check := o.storageCheck
	if check == nil {
		check = hostidentity.CheckStorage
	}
	expected, err := d.StorageExpectation(path)
	if err == nil {
		err = check(expected)
	}
	if errors.Is(err, hostidentity.ErrConfigLocationInadmissible) {
		return fail("config_location_inadmissible", "Keep the private configuration outside the workspace backing filesystem and satisfy its ownership admission.", []string{"select_config", "inspect_config"}, err)
	}
	if err != nil {
		return fail("workspace_storage_unavailable", "The alpha workspace storage is not enrolled or failed identity admission.", []string{"inspect_storage", "attended_storage_setup"}, err)
	}
	r.SelectionAcceptable = true
	host, err := loaded.HostAdmission()
	if err != nil {
		return fail("host_tools_incompatible", "A version-2 host configuration is required.", []string{"inspect_config"}, err)
	}
	if o.HostDoctor == nil {
		return fail("host_tools_incompatible", "Read-only host inspection is unavailable.", []string{"retry_inspection"}, nil)
	}
	report := o.HostDoctor.Doctor(ctx, hostx.Request{ConfiguredStateRoots: host.ConfiguredStateRoots, TartPath: host.Host.TartExecutable, TartHome: host.Host.TartHome, SoftnetPath: host.Host.SoftnetSource})
	switch report.Status {
	case hostx.Healthy:
	case hostx.Missing:
		return fail("host_tools_uninitialized", "Complete attended host installation before creating projects.", []string{"inspect_host", "attended_host_setup"}, nil)
	default:
		return fail("host_tools_incompatible", "Host tools are unsafe, changed, or outside the admitted compatibility set.", []string{"inspect_host"}, nil)
	}
	if o.DomainSetupCheck == nil {
		return fail("domain_incompatible", "Domain admission inspection is unavailable.", []string{"retry_inspection"}, nil)
	}
	initialized, domainErr := o.DomainSetupCheck(ctx, loaded, d)
	if domainErr != nil {
		// CA errors are intentionally not serialized: the inspector never exposes identity material.
		return fail("domain_incompatible", "Domain identity admission failed; inspect existing domain setup before taking action.", []string{"inspect_domain"}, nil)
	}
	if !initialized {
		return fail("domain_uninitialized", "Initialize the alpha domain explicitly before project setup.", []string{"attended_domain_setup"}, nil)
	}

	setup, err := projectx.LoadSetup(d.StateRoot)
	if errors.Is(err, os.ErrNotExist) {
		return fail("project_setup_missing", "Complete attended domain initialization and project asset setup.", []string{"attended_project_setup"}, err)
	}
	if err != nil {
		return fail("project_setup_invalid", "The saved project setup failed admission.", []string{"inspect_project_setup", "attended_project_setup"}, err)
	}
	if o.ProjectSetupCheck == nil {
		return fail("project_setup_invalid", "Project asset inspection is unavailable.", []string{"retry_inspection"}, nil)
	}
	if err := o.ProjectSetupCheck(ctx, d, setup); err != nil {
		return fail("project_setup_invalid", "Project assets are missing, changed, or incompatible; explicitly update the saved setup.", []string{"inspect_project_setup", "attended_project_setup"}, err)
	}
	r.SetupVersion = setup.Version
	r.RecipePreparationAvailable = setup.Version == 2 || setup.Version == 3
	r.Status = "ready"
	r.SelectionAcceptable = true
	r.Guidance = "Alpha project setup prerequisites are admitted. Prepared bases and live project readiness are checked by their operations."
	return r
}
