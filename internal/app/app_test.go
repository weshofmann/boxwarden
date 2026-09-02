package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
)

func TestSessionStatusRendersPersistedAndObservedState(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "status", "dev"}, Options{
		Observer: fake.Observer{Observations: map[string]backend.Observation{
			"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped},
		}},
		Output: &output,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	const want = "domain: work\nsession: dev\nmode: clean\nintended: stopped\nobserved: stopped\ngolden: golden-work-r1\nconsistency: consistent\n"
	if got := output.String(); got != want {
		t.Fatalf("Run() output =\n%s\nwant:\n%s", got, want)
	}
}

func TestSessionStatusAcceptsDomainFromEnvironmentOnlyWhenUnset(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "session", "status", "dev"}, Options{
		Env: []string{"BOXWARDEN_DOMAIN=work"},
		Observer: fake.Observer{Observations: map[string]backend.Observation{
			"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped},
		}},
		Output: &output,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "domain: work\n") {
		t.Fatalf("Run() output = %q, want selected environment domain", output.String())
	}
}

func TestSessionStatusRefusesAnUnknownDomainBeforeObservation(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	observer := &countingObserver{}
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "personal", "session", "status", "dev"}, Options{
		Observer: observer,
		Output:   &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown domain") {
		t.Fatalf("Run() error = %v, want unknown domain", err)
	}
	if observer.calls != 0 {
		t.Fatalf("observer calls = %d, want 0", observer.calls)
	}
}

func TestSessionStatusReportsObserverFailure(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "status", "dev"}, Options{
		Observer: fake.Observer{Err: errors.New("Tart is unavailable")},
		Output:   &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "observe backend object") {
		t.Fatalf("Run() error = %v, want wrapped observer failure", err)
	}
}

func TestSessionStatusRequiresAnExplicitDomain(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	err := Run(context.Background(), []string{"--config", configPath, "session", "status", "dev"}, Options{
		Env:    []string{},
		Output: &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "domain is required") {
		t.Fatalf("Run() error = %v, want explicit-domain error", err)
	}
}

type countingObserver struct {
	calls int
}

func (o *countingObserver) Observe(_ context.Context, objectID string) (backend.Observation, error) {
	o.calls++
	return backend.Observation{ObjectID: objectID, State: backend.ObjectUnknown}, nil
}

func writeStatusFixture(t *testing.T, domain, name string) string {
	t.Helper()
	root := t.TempDir()
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	root = canonicalRoot
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(root, "sessions")
	if err := os.Mkdir(sessions, 0o700); err != nil {
		t.Fatal(err)
	}

	record := `{"version":1,"domain":"` + domain + `","name":"` + name + `","id":"00000000-0000-4000-8000-000000000001","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-work-r1"}`
	if err := os.WriteFile(filepath.Join(sessions, name+".json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(root, "config.json")
	config := `{"version":1,"domains":{"` + domain + `":{"state_root":"` + root + `"}}}`
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}
