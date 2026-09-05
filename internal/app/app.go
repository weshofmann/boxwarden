// Package app composes the Boxwarden control-plane command without binding it
// to a particular VM backend implementation.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type HostInitializer interface {
	Init(context.Context, hostx.Request) (hostx.InitResult, error)
}

type HostDoctor interface {
	Doctor(context.Context, hostx.Request) hostx.Report
}

type CAInitializer interface {
	Init(context.Context, sshx.Domain, []sshx.Domain) (sshx.CAInitResult, error)
}

// Options supplies trusted-host dependencies to Run. App depends only on the
// narrow backend seams and never on Tart directly.
type Options struct {
	ConfigPath string
	Env        []string
	Observer   backend.Observer
	Creator    backend.Creator
	HostInit   HostInitializer
	HostDoctor HostDoctor
	CAInit     CAInitializer
	Output     io.Writer
}

// DefaultConfigPath returns the conventional trusted-host configuration path.
func DefaultConfigPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(base, "boxwarden", "config.json"), nil
}

// Run executes one Boxwarden command. Commands that own domain state require an
// explicit domain; host-global commands deliberately do not select one.
func Run(ctx context.Context, args []string, options Options) error {
	command, err := parseCommand(args, options)
	if err != nil {
		return err
	}
	if options.Output == nil {
		return errors.New("command output is required")
	}

	loadConfig := config.Load
	if command.kind == commandDomainInit {
		loadConfig = config.LoadDomains
	}
	loaded, err := loadConfig(command.configPath)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	var selectedDomain config.Domain
	if command.requiresDomain() {
		selectedDomain, err = loaded.Domain(command.domain)
		if err != nil {
			return err
		}
	}

	switch command.kind {
	case commandInit:
		if options.HostInit == nil {
			return errors.New("host initializer is required")
		}
		hostRequest, err := hostRequest(loaded)
		if err != nil {
			return err
		}
		result, err := options.HostInit.Init(ctx, hostRequest)
		if err != nil {
			return fmt.Errorf("initialize host prerequisites: %w", err)
		}
		return writeInit(options.Output, result)
	case commandDoctor:
		if options.HostDoctor == nil {
			return errors.New("host doctor is required")
		}
		hostRequest, err := hostRequest(loaded)
		if err != nil {
			return err
		}
		report := options.HostDoctor.Doctor(ctx, hostRequest)
		report.Normalize()
		if err := writeDoctor(options.Output, report); err != nil {
			return err
		}
		if report.Status != hostx.Healthy {
			return fmt.Errorf("doctor found %s host prerequisites", report.Status)
		}
		return nil
	case commandDomainInit:
		if options.CAInit == nil {
			return errors.New("domain CA initializer is required")
		}
		selectedCADomain := sshx.Domain{ID: selectedDomain.ID, StateRoot: selectedDomain.StateRoot}
		configuredCADomains := configuredCADomains(loaded)
		result, err := options.CAInit.Init(ctx, selectedCADomain, configuredCADomains)
		if err != nil {
			return fmt.Errorf("initialize domain management CA: %w", err)
		}
		return writeDomainInit(options.Output, command.domain, result.Disposition)
	case commandSessionStatus:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := session.LoadRecord(selectedDomain.StateRoot, command.domain, command.name)
		if err != nil {
			return fmt.Errorf("load session record: %w", err)
		}
		observed, err := options.Observer.Observe(ctx, record.Backend.ObjectID)
		if err != nil {
			return fmt.Errorf("observe backend object %q: %w", record.Backend.ObjectID, err)
		}
		reconciled := lifecycle.Reconcile(record.IntendedState, observed)
		return writeStatus(options.Output, record, observed, reconciled)
	case commandGoldenRegister:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		record, err := golden.Register(ctx, selectedDomain, command.name, options.Observer)
		if err != nil {
			return fmt.Errorf("register golden: %w", err)
		}
		return writeGoldenRegistration(options.Output, record)
	case commandSessionCreate:
		if options.Observer == nil {
			return errors.New("backend observer is required")
		}
		if options.Creator == nil {
			return errors.New("backend creator is required")
		}
		record, err := session.NewService(selectedDomain, options.Observer, options.Creator).Create(ctx, command.name, command.mode)
		if err != nil {
			return fmt.Errorf("create session: %w", err)
		}
		return writeCreatedSession(options.Output, record)
	default:
		return errors.New("unsupported command")
	}
}

type commandKind uint8

const (
	commandSessionStatus commandKind = iota + 1
	commandGoldenRegister
	commandSessionCreate
	commandInit
	commandDoctor
	commandDomainInit
)

type parsedCommand struct {
	kind       commandKind
	configPath string
	domain     string
	name       string
	mode       session.Mode
}

func (c parsedCommand) requiresDomain() bool {
	return c.kind != commandInit && c.kind != commandDoctor
}

func parseCommand(args []string, options Options) (parsedCommand, error) {
	configPath := options.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return parsedCommand{}, err
		}
	}

	set := flag.NewFlagSet("boxwarden", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	domain := set.String("domain", environmentValue(options.Env, "BOXWARDEN_DOMAIN"), "security domain")
	config := set.String("config", configPath, "configuration file")
	if err := set.Parse(args); err != nil {
		return parsedCommand{}, fmt.Errorf("parse command: %w", err)
	}
	if strings.TrimSpace(*config) == "" {
		return parsedCommand{}, errors.New("configuration path is required")
	}
	explicitDomain := false
	set.Visit(func(flag *flag.Flag) {
		if flag.Name == "domain" {
			explicitDomain = true
		}
	})

	remaining := set.Args()
	base := parsedCommand{configPath: *config}
	if len(remaining) == 1 && (remaining[0] == "init" || remaining[0] == "doctor") {
		if explicitDomain {
			return parsedCommand{}, fmt.Errorf("--domain is not accepted for host-global command %q", remaining[0])
		}
		if remaining[0] == "init" {
			base.kind = commandInit
		} else {
			base.kind = commandDoctor
		}
		return base, nil
	}
	if strings.TrimSpace(*domain) == "" {
		return parsedCommand{}, errors.New("domain is required; pass --domain or set BOXWARDEN_DOMAIN")
	}
	base.domain = *domain
	if len(remaining) == 3 && remaining[0] == "session" && remaining[1] == "status" {
		base.kind = commandSessionStatus
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) == 2 && remaining[0] == "domain" && remaining[1] == "init" {
		base.kind = commandDomainInit
		return base, nil
	}
	if len(remaining) == 3 && remaining[0] == "golden" && remaining[1] == "register" {
		base.kind = commandGoldenRegister
		base.name = remaining[2]
		return base, nil
	}
	if len(remaining) >= 3 && remaining[0] == "session" && remaining[1] == "create" {
		createSet := flag.NewFlagSet("session create", flag.ContinueOnError)
		createSet.SetOutput(io.Discard)
		mode := createSet.String("mode", string(session.ModeClean), "session mode")
		if err := createSet.Parse(remaining[2:]); err != nil {
			return parsedCommand{}, fmt.Errorf("parse session create: %w", err)
		}
		if len(createSet.Args()) != 1 {
			return parsedCommand{}, errors.New("session create requires one validated session name")
		}
		base.mode = session.Mode(*mode)
		if base.mode != session.ModeClean && base.mode != session.ModeQuarantine {
			return parsedCommand{}, fmt.Errorf("invalid session mode %q", base.mode)
		}
		base.kind = commandSessionCreate
		base.name = createSet.Args()[0]
		return base, nil
	}
	return parsedCommand{}, errors.New("supported commands are: init, doctor, domain init, golden register <object>, session create [--mode clean|quarantine] <session>, session status <session>")
}

func writeInit(output io.Writer, result hostx.InitResult) error {
	if _, err := fmt.Fprintf(output, "host-installed: %t\nrefresh-login-session: %t\n", result.HostInstalled, result.RefreshLoginSession); err != nil {
		return fmt.Errorf("write init result: %w", err)
	}
	return nil
}

func writeDomainInit(output io.Writer, domain string, disposition sshx.CAInitDisposition) error {
	var status string
	switch disposition {
	case sshx.CAInitialized:
		status = "initialized"
	case sshx.CAAlreadyInitialized:
		status = "already initialized"
	default:
		return fmt.Errorf("domain CA initializer returned unknown disposition %q", disposition)
	}
	if _, err := fmt.Fprintf(output, "domain: %s\nmanagement-ca: %s\n", domain, status); err != nil {
		return fmt.Errorf("write domain init result: %w", err)
	}
	return nil
}

func hostRequest(loaded config.Config) (hostx.Request, error) {
	admission, err := loaded.HostAdmission()
	if err != nil {
		return hostx.Request{}, err
	}
	return hostx.Request{ConfiguredStateRoots: admission.ConfiguredStateRoots, TartPath: admission.Host.TartExecutable, TartHome: admission.Host.TartHome, SoftnetPath: admission.Host.SoftnetSource}, nil
}

func configuredCADomains(loaded config.Config) []sshx.Domain {
	configured := loaded.Domains()
	domains := make([]sshx.Domain, 0, len(configured))
	for _, configuredDomain := range configured {
		domains = append(domains, sshx.Domain{ID: configuredDomain.ID, StateRoot: configuredDomain.StateRoot})
	}
	return domains
}

func writeDoctor(output io.Writer, report hostx.Report) error {
	report.Normalize()
	if _, err := fmt.Fprintf(output, "status: %s\n", report.Status); err != nil {
		return fmt.Errorf("write doctor report: %w", err)
	}
	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(output, "%s: [%s] observed=%s expected=%s remedy=%s\n", finding.Code, finding.Category, finding.Observed, finding.Expected, finding.Remedy); err != nil {
			return fmt.Errorf("write doctor finding: %w", err)
		}
	}
	return nil
}

func environmentValue(environment []string, key string) string {
	if environment == nil {
		environment = os.Environ()
	}
	prefix := key + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func writeStatus(output io.Writer, record session.Record, observed backend.Observation, reconciled lifecycle.Reconciliation) error {
	observedState := string(observed.State)
	if !observed.Exists {
		observedState = "missing"
	}
	golden := record.GoldenRevision
	if golden == "" {
		golden = "(none)"
	}
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nmode: %s\nintended: %s\nobserved: %s\ngolden: %s\nconsistency: %s\n", record.Domain, record.Name, record.Mode, record.IntendedState, observedState, golden, reconciled.Consistency); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	if observed.Diagnostic != "" {
		if _, err := fmt.Fprintf(output, "backend-diagnostic: %s\n", observed.Diagnostic); err != nil {
			return fmt.Errorf("write backend diagnostic: %w", err)
		}
	}
	if reconciled.Diagnostic != "" {
		if _, err := fmt.Fprintf(output, "diagnostic: %s\n", reconciled.Diagnostic); err != nil {
			return fmt.Errorf("write reconciliation diagnostic: %w", err)
		}
	}
	return nil
}

func writeGoldenRegistration(output io.Writer, record golden.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\ngolden: %s\nstate: registered\n", record.Domain, record.Revision); err != nil {
		return fmt.Errorf("write golden registration: %w", err)
	}
	return nil
}

func writeCreatedSession(output io.Writer, record session.Record) error {
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nmode: %s\nstate: %s\n", record.Domain, record.Name, record.Mode, record.IntendedState); err != nil {
		return fmt.Errorf("write created session: %w", err)
	}
	return nil
}
