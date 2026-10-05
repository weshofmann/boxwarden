package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostidentity"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/privateacl"
)

const firstRunReserveBytes uint64 = (20 << 30) + (512 << 20)

type firstRunDependencies struct {
	home         func() (string, error)
	location     func(string, string) (hostidentity.LocationObservation, error)
	observe      func(*os.File) (hostidentity.Identity, error)
	alternatives func(string) ([]string, error)
	host         func(context.Context, string, string) (config.Host, hostx.Report, string, error)
	resources    func(context.Context, app.FirstRunInput) (app.SetupPrepareInput, string, error)
	publish      func(hostidentity.StorageExpectation, []byte) error
	prepare      app.SetupPrepareFunc
}

func productionFirstRunDependencies() firstRunDependencies {
	return firstRunDependencies{home: os.UserHomeDir, location: hostidentity.ObserveFirstRunLocation, observe: hostidentity.Observe, alternatives: hostidentity.MountedFirstRunLocations, host: inspectFirstRunHost, resources: preflightFirstRunResources, publish: hostidentity.WriteEnrolledConfig, prepare: prepareSetup}
}

func planFirstRun(ctx context.Context, input app.FirstRunInput) (app.FirstRunPlan, error) {
	plan, _, _, err := buildFirstRunPlan(ctx, input, productionFirstRunDependencies())
	return plan, err
}

func createFirstRun(ctx context.Context, input app.FirstRunInput, out io.Writer) (app.SetupInspection, bool, error) {
	return createFirstRunWithDependencies(ctx, input, out, productionFirstRunDependencies())
}

func canonicalFirstRunUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9') && !(c >= 'a' && c <= 'f') {
			return false
		}
	}
	return value != "00000000-0000-0000-0000-000000000000"
}

func buildFirstRunPlan(ctx context.Context, input app.FirstRunInput, d firstRunDependencies) (app.FirstRunPlan, config.Host, app.SetupPrepareInput, error) {
	plan := app.FirstRunPlan{Version: 1, Scope: "alpha_first_run", SetupID: input.SetupID, DataLocation: input.DataLocation, PackageRoot: input.PackageRoot, ISOPath: input.ISOPath, ReserveBytes: firstRunReserveBytes, HostStatus: "unchecked", Alternatives: []string{}, Prerequisites: []string{}, NextActions: []string{}}
	var host config.Host
	var resources app.SetupPrepareInput
	fail := func(status, guidance string, err error) (app.FirstRunPlan, config.Host, app.SetupPrepareInput, error) {
		plan.Status = status
		plan.Guidance = guidance
		plan.NextActions = []string{"review_prerequisites", "replan"}
		if err != nil {
			plan.Diagnostic = err.Error()
			if len(plan.Diagnostic) > 4096 {
				plan.Diagnostic = plan.Diagnostic[:4096]
			}
		}
		return plan, host, resources, nil
	}
	if err := ctx.Err(); err != nil {
		return plan, host, resources, err
	}
	if !canonicalFirstRunUUID(input.SetupID) {
		return fail("invalid_request", "First-run setup requires a fresh canonical lowercase UUID.", nil)
	}
	home, err := d.home()
	if err != nil {
		return fail("config_location_inadmissible", "The operator home cannot be resolved.", err)
	}
	if !canonicalProjectSetupPath(home) {
		return fail("config_location_inadmissible", "The operator home must be a direct absolute location.", nil)
	}
	plan.ConfigPath = filepath.Join(home, "Library", "Application Support", "boxwarden", "setups", input.SetupID, "config.json")
	ancestor, err := firstRunConfigAncestor(home, plan.ConfigPath)
	if err != nil {
		return fail("config_location_inadmissible", "The default private configuration hierarchy failed admission.", err)
	}
	if alternatives, err := d.alternatives(ancestor); err == nil {
		plan.Alternatives = alternatives
	}
	if input.DataLocation == "" {
		return fail("data_location_required", "Choose an existing private directory on a separate mounted APFS volume. Existing entries will be preserved.", nil)
	}
	if !canonicalProjectSetupPath(input.DataLocation) {
		return fail("data_location_inadmissible", "Choose a direct clean absolute data location.", nil)
	}
	plan.StateRoot = filepath.Join(input.DataLocation, "boxwarden-setup-"+input.SetupID, "alpha")
	observation, err := d.location(input.DataLocation, ancestor)
	if err != nil {
		return fail("data_location_inadmissible", "The data location requires operator ownership, mode 0700, no extended ACL or symlink ancestry, and a separate mounted APFS filesystem.", err)
	}
	plan.MountPoint = observation.MountPoint
	plan.VolumeUUID = observation.VolumeUUID
	plan.AvailableBytes = observation.AvailableBytes
	plan.ExistingEntries = observation.ExistingEntries
	plan.DataFileID = observation.FileID
	plan.ConfigAncestorFileID = observation.OutputFileID
	plan.ConfigAncestorVolumeUUID = observation.OutputVolumeUUID
	plan.ConfigAncestor = ancestor
	plan.ReserveBytes = firstRunHeadroom(observation.CapacityBytes)
	if observation.AvailableBytes < plan.ReserveBytes {
		return fail("insufficient_space", "The data location needs free space above max(20 GiB, 10% capacity), plus 512 MiB for an initial workspace. VM and project capacity is checked separately.", nil)
	}
	if observation.OutputCapacityBytes > 0 && observation.OutputAvailableBytes < firstRunHeadroom(observation.OutputCapacityBytes) {
		return fail("insufficient_space", "The host Data filesystem must retain max(20 GiB, 10% capacity) headroom plus space for configuration and preparation assets.", nil)
	}
	for _, path := range []string{filepath.Dir(plan.ConfigPath), filepath.Dir(plan.StateRoot)} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			return fail("target_collision", "The new setup target already exists or cannot be safely inspected. Generate a new setup ID and replan.", err)
		}
	}
	var report hostx.Report
	var hostFingerprint string
	host, report, hostFingerprint, err = d.host(ctx, home, plan.StateRoot)
	plan.HostStatus = string(report.Status)
	for i, finding := range report.Findings {
		if i >= 32 {
			break
		}
		plan.Prerequisites = append(plan.Prerequisites, finding.Code+": "+finding.Remedy)
	}
	if err != nil || report.Status != hostx.Healthy {
		status := "host_tools_incompatible"
		if report.Status == hostx.Missing {
			status = "host_tools_uninitialized"
		}
		return fail(status, "Complete attended host setup for the exact qualified Tart and Softnet toolchain, then replan. First-run setup never installs or repairs host tools.", err)
	}
	plan.Prerequisites = append(plan.Prerequisites, "Qualified host toolchain passed read-only doctor inspection.")
	if input.ISOPath == "" {
		return fail("iso_required", "Choose the pinned Ubuntu installer before reviewing the complete plan.", nil)
	}
	var resourceFingerprint string
	resources, resourceFingerprint, err = d.resources(ctx, input)
	if err != nil {
		return fail("package_prerequisites_unavailable", "The package, installer, or local preparation tools failed admission. Resolve the displayed prerequisite and replan.", err)
	}
	plan.ReserveBytes += resources.BootstrapBytes
	if observation.AvailableBytes <= plan.ReserveBytes {
		return fail("insufficient_space", "Cold first-project preparation needs space for two installer images and the existing stopping margin, in addition to the storage floor and workspace. Choose a larger compatible data location; no existing data was changed.", nil)
	}
	plan.Prerequisites = append(plan.Prerequisites, "Verified Ubuntu installer and packaged workspace helpers passed admission.", "Existing OpenSSL and xorriso passed capability and exact-byte checks; no compiler or download is needed for setup.", "Configuration will be created privately on the host filesystem.", "Only a new setup directory will be added to the selected data location.")
	if _, err := config.NewEnrolledAlphaConfig(host, plan.StateRoot, config.WorkspaceStorage{MountPoint: plan.MountPoint, VolumeUUID: plan.VolumeUUID}); err != nil {
		return fail("invalid_request", "The planned host and storage locations overlap or are invalid.", err)
	}
	if err := ctx.Err(); err != nil {
		return plan, host, resources, err
	}
	// Free bytes may change between review and execution; admission checks the
	// reserve again. Bind durable identity, targets, and tool/resource inputs.
	observation.AvailableBytes = 0
	observation.OutputAvailableBytes = 0
	bound := struct {
		Version                               int
		Input                                 app.FirstRunInput
		ConfigPath, StateRoot, ConfigAncestor string
		Location                              hostidentity.LocationObservation
		Host                                  config.Host
		HostFingerprint, ResourceFingerprint  string
		ReserveBytes                          uint64
	}{1, input, plan.ConfigPath, plan.StateRoot, ancestor, observation, host, hostFingerprint, resourceFingerprint, plan.ReserveBytes}
	bound.Input.ExpectedDigest = ""
	raw, err := json.Marshal(bound)
	if err != nil {
		return plan, host, resources, err
	}
	digest := sha256.Sum256(raw)
	plan.ExpectedDigest = hex.EncodeToString(digest[:])
	plan.Status = "ready"
	plan.Guidance = "Create this new alpha configuration and prepare its local project assets. Existing configurations and selected-location entries will be preserved."
	plan.NextActions = []string{"create_setup"}
	return plan, host, resources, nil
}

func firstRunHeadroom(capacity uint64) uint64 {
	floor := capacity / 10
	if floor < 20<<30 {
		floor = 20 << 30
	}
	return floor + (512 << 20)
}

func firstRunConfigAncestor(home, configPath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil || resolved != home {
		return "", errors.New("operator home unavailable or has symlink ancestry")
	}
	if err := privateacl.CheckAncestorChain(home); err != nil {
		return "", err
	}
	current := home
	parts := []string{"Library", "Application Support", "boxwarden", "setups", filepath.Base(filepath.Dir(configPath))}
	for i, part := range parts {
		next := filepath.Join(current, part)
		info, err := os.Lstat(next)
		if errors.Is(err, os.ErrNotExist) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("configuration ancestor must be a direct directory")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0022 != 0 {
			return "", errors.New("configuration ancestor owner or mode is unsafe")
		}
		if i >= 2 && info.Mode().Perm() != 0700 {
			return "", errors.New("existing configuration hierarchy must be private mode 0700")
		}
		checkACL := privateacl.Check
		if i < 2 {
			if err := privateacl.CheckSafeAncestor(next, info, privateacl.OSInspector{}); err != nil {
				return "", err
			}
		} else if err := checkACL(next, info, privateacl.OSInspector{}); err != nil {
			return "", err
		}
		current = next
	}
	return current, nil
}

func inspectFirstRunHost(ctx context.Context, home, root string) (config.Host, hostx.Report, string, error) {
	var host config.Host
	inspector := hostx.NewOSDoctorInspector()
	manifestPath := filepath.Join(filepath.Dir(hostx.QualifiedSoftnetPath), "manifest.json")
	fact, err := inspector.InspectPath(manifestPath)
	if err != nil {
		return host, hostx.Report{Status: hostx.Drifted}, "", err
	}
	if !fact.Exists {
		return host, hostx.Report{Status: hostx.Missing}, "", nil
	}
	if !fact.Regular || fact.UID != 0 || fact.GID != 0 || fact.Mode != 0444 || fact.Links != 1 || fact.ExtendedACL {
		return host, hostx.Report{Status: hostx.Drifted}, "", errors.New("public host manifest failed admission")
	}
	manifest, err := hostx.ParseManifest(fact.Data)
	if err != nil {
		return host, hostx.Report{Status: hostx.Drifted}, "", errors.New("public host manifest is invalid or outside the stock compatibility set")
	}
	if manifest.Operator.UID != os.Getuid() || manifest.Operator.Home != home || manifest.Tart.Version != hostx.TartVersion || manifest.Tart.ExecutableSHA256 != hostx.TartExecutableSHA256 || manifest.Tart.ArchiveSHA256 != hostx.TartArchiveSHA256 {
		return host, hostx.Report{Status: hostx.Drifted}, "", errors.New("public manifest does not bind this operator and qualified stock Tart")
	}
	host = config.Host{TartExecutable: manifest.Tart.Path, TartHome: manifest.TartHome, SoftnetSource: hostx.QualifiedSoftnetPath}
	report := hostx.NewSystemDoctor().Doctor(ctx, hostx.Request{ConfiguredStateRoots: []string{root}, TartPath: host.TartExecutable, TartHome: host.TartHome, SoftnetPath: host.SoftnetSource})
	digest := sha256.Sum256(fact.Data)
	return host, report, hex.EncodeToString(digest[:]), nil
}

func createFirstRunWithDependencies(ctx context.Context, input app.FirstRunInput, out io.Writer, d firstRunDependencies) (app.SetupInspection, bool, error) {
	var result app.SetupInspection
	if len(input.ExpectedDigest) != 64 {
		return result, false, errors.New("creation requires the reviewed plan digest")
	}
	if _, err := hex.DecodeString(input.ExpectedDigest); err != nil || strings.ToLower(input.ExpectedDigest) != input.ExpectedDigest {
		return result, false, errors.New("creation requires canonical SHA-256 plan digest")
	}
	plan, host, resources, err := buildFirstRunPlan(ctx, input, d)
	if err != nil {
		return result, false, err
	}
	if plan.Status != "ready" {
		return result, false, fmt.Errorf("first-run prerequisites are %s; %s", plan.Status, plan.Guidance)
	}
	if input.ExpectedDigest != plan.ExpectedDigest {
		return result, false, errors.New("first-run plan is stale; inspect and review a new plan")
	}
	if err := ctx.Err(); err != nil {
		return result, false, err
	}
	raw, err := config.NewEnrolledAlphaConfig(host, plan.StateRoot, config.WorkspaceStorage{MountPoint: plan.MountPoint, VolumeUUID: plan.VolumeUUID})
	if err != nil {
		return result, false, err
	}
	changed := false
	failure := func(err error) (app.SetupInspection, bool, error) {
		if changed {
			err = fmt.Errorf("new setup creation may have retained state at %s and %s; inspect these exact targets before retrying: %w", plan.ConfigPath, plan.StateRoot, err)
		}
		return result, changed, err
	}
	if err := createFirstRunDirectories(ctx, plan, d.observe, &changed); err != nil {
		return failure(err)
	}
	if err := ctx.Err(); err != nil {
		return failure(err)
	}
	if err := d.publish(hostidentity.StorageExpectation{ConfigPath: plan.ConfigPath, StateRoot: plan.StateRoot, MountPoint: plan.MountPoint, VolumeUUID: plan.VolumeUUID}, raw); err != nil {
		return failure(err)
	}
	if err := ctx.Err(); err != nil {
		return failure(err)
	}
	// Enrollment is authoritative only after the existing exclusive durable
	// publisher succeeds. Preparation is explicit and receives this new config.
	result, _, err = d.prepare(ctx, plan.ConfigPath, resources, out)
	if err != nil {
		return failure(err)
	}
	if result.ConfigPath != plan.ConfigPath {
		return failure(errors.New("preparation returned a different configuration target"))
	}
	return result, true, nil
}

func createFirstRunDirectories(ctx context.Context, plan app.FirstRunPlan, observe func(*os.File) (hostidentity.Identity, error), changed *bool) error {
	if observe == nil {
		return errors.New("pinned APFS directory identity inspection is unavailable")
	}
	configRoot, err := os.OpenRoot(plan.ConfigAncestor)
	if err != nil {
		return err
	}
	defer configRoot.Close()
	dataRoot, err := os.OpenRoot(plan.DataLocation)
	if err != nil {
		return err
	}
	defer dataRoot.Close()
	for _, item := range []struct {
		root       *os.Root
		path       string
		id         uint64
		volumeUUID string
		private    bool
	}{{configRoot, plan.ConfigAncestor, plan.ConfigAncestorFileID, plan.ConfigAncestorVolumeUUID, false}, {dataRoot, plan.DataLocation, plan.DataFileID, plan.VolumeUUID, true}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := item.root.Stat(".")
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || uint64(stat.Ino) != item.id || int(stat.Uid) != os.Getuid() || info.Mode().Perm()&0022 != 0 || item.private && info.Mode().Perm() != 0700 {
			return errors.New("planned directory identity, ownership, or mode changed before creation")
		}
		// Inodes are scoped to one filesystem. Recheck the durable APFS volume
		// and permanent object identity on the opened root before either mkdir.
		pinned, err := item.root.Open(".")
		if err != nil {
			return err
		}
		pinnedInfo, statErr := pinned.Stat()
		if statErr != nil || !os.SameFile(info, pinnedInfo) {
			pinned.Close()
			return fmt.Errorf("opened directory changed before APFS inspection: %v", statErr)
		}
		identity, observeErr := observe(pinned)
		closeErr := pinned.Close()
		if observeErr != nil {
			return fmt.Errorf("recheck planned APFS directory identity: %w", observeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if identity.VolumeUUID != item.volumeUUID || identity.FileID != item.id {
			return errors.New("planned APFS volume or permanent directory file ID changed before creation")
		}
		if err := privateacl.CheckAncestorChain(filepath.Dir(item.path)); err != nil {
			return err
		}
		if item.private {
			if err := privateacl.Check(item.path, info, privateacl.OSInspector{}); err != nil {
				return err
			}
		} else if err := privateacl.CheckSafeAncestor(item.path, info, privateacl.OSInspector{}); err != nil {
			return err
		}
	}
	relative, err := filepath.Rel(plan.ConfigAncestor, filepath.Dir(plan.ConfigPath))
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return errors.New("fresh configuration target escaped its planned ancestor")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	current := "."
	for _, part := range parts {
		if err := ctx.Err(); err != nil {
			return err
		}
		next := filepath.Join(current, part)
		// Every component below the nearest existing ancestor was absent during
		// planning. An appearing directory is a collision, never an adoption.
		if err := configRoot.Mkdir(next, 0700); err != nil {
			return err
		}
		*changed = true
		if err := syncFirstRunRootDirectory(configRoot, current); err != nil {
			return err
		}
		current = next
	}
	if err := syncFirstRunRootDirectory(configRoot, current); err != nil {
		return err
	}
	parent := filepath.Base(filepath.Dir(plan.StateRoot))
	for _, item := range []struct{ path, parent string }{{parent, "."}, {filepath.Join(parent, "alpha"), parent}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := dataRoot.Mkdir(item.path, 0700); err != nil {
			return err
		}
		*changed = true
		if err := syncFirstRunRootDirectory(dataRoot, item.parent); err != nil {
			return err
		}
	}
	return syncFirstRunRootDirectory(dataRoot, filepath.Join(parent, "alpha"))
}

func syncFirstRunRootDirectory(root *os.Root, path string) error {
	directory, err := root.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
