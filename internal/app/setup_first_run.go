package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
)

type FirstRunInput struct {
	SetupID        string `json:"setup_id"`
	DataLocation   string `json:"data_location"`
	PackageRoot    string `json:"package_root"`
	ISOPath        string `json:"iso_path"`
	ExpectedDigest string `json:"expected_digest"`
}

// FirstRunPlan conveys prerequisites and bounded fresh targets. It is not
// enrollment or runtime authority; create independently rechecks the digest.
type FirstRunPlan struct {
	Version         int      `json:"version"`
	Scope           string   `json:"scope"`
	SetupID         string   `json:"setup_id"`
	DataLocation    string   `json:"data_location"`
	PackageRoot     string   `json:"package_root"`
	ISOPath         string   `json:"iso_path"`
	ConfigPath      string   `json:"config_path"`
	StateRoot       string   `json:"state_root"`
	MountPoint      string   `json:"mount_point"`
	VolumeUUID      string   `json:"volume_uuid"`
	AvailableBytes  uint64   `json:"available_bytes"`
	ReserveBytes    uint64   `json:"reserve_bytes"`
	HostStatus      string   `json:"host_status"`
	Status          string   `json:"status"`
	Guidance        string   `json:"guidance"`
	ExpectedDigest  string   `json:"expected_digest"`
	ExistingEntries int      `json:"existing_entries"`
	Alternatives    []string `json:"alternatives"`
	Prerequisites   []string `json:"prerequisites"`
	NextActions     []string `json:"next_actions"`
	Diagnostic      string   `json:"diagnostic,omitempty"`
	// Creation-only pinned directory observations are never part of the UI API.
	DataFileID               uint64 `json:"-"`
	ConfigAncestorFileID     uint64 `json:"-"`
	ConfigAncestorVolumeUUID string `json:"-"`
	ConfigAncestor           string `json:"-"`
}

type SetupFirstRunPlanFunc func(context.Context, FirstRunInput) (FirstRunPlan, error)
type SetupFirstRunCreateFunc func(context.Context, FirstRunInput, io.Writer) (SetupInspection, bool, error)

func runSetupFirstRun(ctx context.Context, args []string, o Options) (handled bool, err error) {
	globals := flag.NewFlagSet("setup", flag.ContinueOnError)
	globals.SetOutput(io.Discard)
	configPath := globals.String("config", "", "configuration")
	domain := globals.String("domain", "", "domain")
	if globals.Parse(args) != nil {
		return false, nil
	}
	rest := globals.Args()
	if len(rest) < 2 || rest[0] != "setup" || (rest[1] != "plan" && rest[1] != "create") {
		return false, nil
	}
	if o.Output == nil {
		return true, errors.New("command output is required")
	}
	stream := &projectJSON{output: o.Output, operation: "setup.create"}
	uncertain := false
	defer func() {
		if rest[1] == "create" && err != nil {
			err = errors.Join(err, stream.emit("error", err.Error(), map[string]any{"uncertain": uncertain}))
		}
	}()
	explicitGlobal := false
	globals.Visit(func(*flag.Flag) { explicitGlobal = true })
	if explicitGlobal || *configPath != "" || *domain != "" {
		return true, errors.New("first-run setup derives a new alpha configuration and does not accept --config or --domain")
	}
	var input FirstRunInput
	flags := flag.NewFlagSet("setup "+rest[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	jsonMode := flags.Bool("json", false, "structured output")
	flags.StringVar(&input.SetupID, "setup-id", "", "new setup UUID")
	flags.StringVar(&input.DataLocation, "data-location", "", "selected existing mounted data directory")
	flags.StringVar(&input.PackageRoot, "package", "", "current package root")
	flags.StringVar(&input.ISOPath, "iso", "", "Ubuntu installer")
	flags.StringVar(&input.ExpectedDigest, "expected-digest", "", "reviewed plan digest")
	if err = flags.Parse(rest[2:]); err != nil {
		return true, err
	}
	if !*jsonMode || len(flags.Args()) != 0 || input.SetupID == "" || input.PackageRoot == "" {
		return true, errors.New("setup plan/create requires --json --setup-id UUID --package ROOT")
	}
	if err = ctx.Err(); err != nil {
		return true, err
	}
	if rest[1] == "plan" {
		if input.ExpectedDigest != "" {
			return true, errors.New("setup plan does not accept --expected-digest")
		}
		if o.SetupFirstRunPlan == nil {
			return true, errors.New("first-run planning unavailable")
		}
		var result FirstRunPlan
		result, err = o.SetupFirstRunPlan(ctx, input)
		if err != nil {
			return true, err
		}
		if err = ctx.Err(); err != nil {
			return true, err
		}
		return true, json.NewEncoder(o.Output).Encode(result)
	}
	if input.DataLocation == "" || input.ISOPath == "" || input.ExpectedDigest == "" {
		return true, errors.New("setup create requires selected --data-location --iso and --expected-digest")
	}
	if o.SetupFirstRunCreate == nil {
		return true, errors.New("first-run creation unavailable")
	}
	var result SetupInspection
	result, uncertain, err = o.SetupFirstRunCreate(ctx, input, stream)
	if err != nil {
		return true, err
	}
	return true, stream.emit("result", "", result)
}
