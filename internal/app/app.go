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
	"github.com/weshofmann/boxwarden/internal/lifecycle"
	"github.com/weshofmann/boxwarden/internal/session"
)

// Options supplies trusted-host dependencies to Run. Observer must be a
// read-only backend implementation; app deliberately depends only on the
// backend seam, never on Tart directly.
type Options struct {
	ConfigPath string
	Env        []string
	Observer   backend.Observer
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

// Run executes a single Boxwarden command. V0.1 supports only the read-only
// session status operation.
func Run(ctx context.Context, args []string, options Options) error {
	command, err := parseCommand(args, options)
	if err != nil {
		return err
	}
	if options.Observer == nil {
		return errors.New("backend observer is required")
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
}

type statusCommand struct {
	configPath string
	domain     string
	name       string
}

func parseCommand(args []string, options Options) (statusCommand, error) {
	configPath := options.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = DefaultConfigPath()
		if err != nil {
			return statusCommand{}, err
		}
	}

	set := flag.NewFlagSet("boxwarden", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	domain := set.String("domain", environmentValue(options.Env, "BOXWARDEN_DOMAIN"), "security domain")
	config := set.String("config", configPath, "configuration file")
	if err := set.Parse(args); err != nil {
		return statusCommand{}, fmt.Errorf("parse command: %w", err)
	}
	if strings.TrimSpace(*domain) == "" {
		return statusCommand{}, errors.New("domain is required; pass --domain or set BOXWARDEN_DOMAIN")
	}

	remaining := set.Args()
	if len(remaining) != 3 || remaining[0] != "session" || remaining[1] != "status" {
		return statusCommand{}, errors.New("supported command is: boxwarden --domain <id> session status <session>")
	}
	if strings.TrimSpace(*config) == "" {
		return statusCommand{}, errors.New("configuration path is required")
	}
	return statusCommand{configPath: *config, domain: *domain, name: remaining[2]}, nil
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
