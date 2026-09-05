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
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Options supplies trusted-host dependencies to Run. App depends only on the
// narrow backend seams and never on Tart directly.
type Options struct {
	ConfigPath string
	Env        []string
	Observer   backend.Observer
	Creator    backend.Creator
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

// Run executes one domain-scoped Boxwarden command.
func Run(ctx context.Context, args []string, options Options) error {
	command, err := parseCommand(args, options)
	if err != nil {
		return err
	}
	if options.Output == nil {
		return errors.New("command output is required")
	}

	loaded, err := config.Load(command.configPath)
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	selectedDomain, err := loaded.Domain(command.domain)
	if err != nil {
		return err
	}

	switch command.kind {
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
)

type parsedCommand struct {
	kind       commandKind
	configPath string
	domain     string
	name       string
	mode       session.Mode
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
	if strings.TrimSpace(*domain) == "" {
		return parsedCommand{}, errors.New("domain is required; pass --domain or set BOXWARDEN_DOMAIN")
	}
	if strings.TrimSpace(*config) == "" {
		return parsedCommand{}, errors.New("configuration path is required")
	}

	remaining := set.Args()
	base := parsedCommand{configPath: *config, domain: *domain}
	if len(remaining) == 3 && remaining[0] == "session" && remaining[1] == "status" {
		base.kind = commandSessionStatus
		base.name = remaining[2]
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
	return parsedCommand{}, errors.New("supported commands are: golden register <object>, session create [--mode clean|quarantine] <session>, session status <session>")
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
