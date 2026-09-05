package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

func TestSessionStatusRendersPersistedAndObservedState(t *testing.T) {
	configPath := writeStatusFixture(t, "work", "dev")
	recordPath := filepath.Join(filepath.Dir(configPath), "sessions", "dev.json")
	before, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "status", "dev"}, Options{
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
	after, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("session status rewrote version 1 record = %q, want %q", after, before)
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

func TestSessionStatusReportsMissingCreatingSessionAsIndeterminate(t *testing.T) {
	configPath, domainConfig := writeDomainFixture(t, "work")
	if err := os.Mkdir(filepath.Join(domainConfig.StateRoot, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	record := `{"version":2,"domain":"work","name":"dev","id":"00112233-4455-4677-8899-aabbccddeeff","mode":"clean","intended_state":"creating","backend":{"kind":"tart","object_id":"boxwarden-work-00112233445546778899aabbccddeeff"},"golden_revision":"golden-work-r1","readiness":{"status":"not_ready","diagnostic":""}}`
	if err := os.WriteFile(filepath.Join(domainConfig.StateRoot, "sessions", "dev.json"), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "status", "dev"}, Options{
		Observer: fake.New(),
		Output:   &output,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(output.String(), "consistency: indeterminate\n") || !strings.Contains(output.String(), "transitional") {
		t.Fatalf("Run() output = %q, want actionable transitional reconciliation", output.String())
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

func TestHostCommandsAreDomainlessAndDoNotTouchDomainCA(t *testing.T) {
	configPath := writeV2DomainSetFixture(t)
	host := &hostServiceFake{}
	ca := &caStoreFake{}
	var initOutput bytes.Buffer
	if err := Run(context.Background(), []string{"--config", configPath, "init"}, Options{Env: []string{"BOXWARDEN_DOMAIN=work"}, HostInit: host, CAInit: ca, Output: &initOutput}); err != nil {
		t.Fatalf("Run(init) error = %v", err)
	}
	if host.initCalls != 1 || host.doctorCalls != 0 || ca.initCalls != 0 {
		t.Fatalf("init calls = host init %d doctor %d CA init %d, want host init only", host.initCalls, host.doctorCalls, ca.initCalls)
	}
	if got, want := initOutput.String(), "host-installed: true\nrefresh-login-session: false\n"; got != want {
		t.Fatalf("init output = %q, want %q", got, want)
	}
	if len(host.request.ConfiguredStateRoots) != 2 || host.request.ConfiguredStateRoots[0] == "" || host.request.ConfiguredStateRoots[1] == "" {
		t.Fatalf("host request roots = %#v, want complete host-global collection", host.request.ConfiguredStateRoots)
	}

	var doctorOutput bytes.Buffer
	if err := Run(context.Background(), []string{"--config", configPath, "doctor"}, Options{Env: []string{"BOXWARDEN_DOMAIN=work"}, HostDoctor: host, Output: &doctorOutput}); err != nil {
		t.Fatalf("Run(doctor) error = %v", err)
	}
	if host.initCalls != 1 || host.doctorCalls != 1 || ca.initCalls != 0 {
		t.Fatalf("doctor calls = host init %d doctor %d CA init %d, want host doctor only", host.initCalls, host.doctorCalls, ca.initCalls)
	}
	if got, want := doctorOutput.String(), "status: healthy\n"; got != want {
		t.Fatalf("doctor output = %q, want %q", got, want)
	}
}

func TestHostCommandsReceiveExactSortedCompleteConfiguredRoots(t *testing.T) {
	stateBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := make(map[string]string, 3)
	for _, name := range []string{"alpha", "personal", "work"} {
		root := filepath.Join(stateBase, name)
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		roots[name] = root
	}
	tart := filepath.Join(hostBase, "tart")
	softnet := filepath.Join(hostBase, "softnet")
	home := filepath.Join(hostBase, "tart-home")
	for _, path := range []string{tart, softnet} {
		if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(stateBase, "config.json")
	contents := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q},"alpha":{"state_root":%q},"personal":{"state_root":%q}}}`, tart, home, softnet, roots["work"], roots["alpha"], roots["personal"])
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &hostServiceFake{}
	if err := Run(context.Background(), []string{"--config", configPath, "init"}, Options{HostInit: host, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Run(init) error = %v", err)
	}
	if got, want := fmt.Sprint(host.request.ConfiguredStateRoots), fmt.Sprint([]string{roots["alpha"], roots["personal"], roots["work"]}); got != want {
		t.Fatalf("host request roots = %s, want %s", got, want)
	}
}

func TestHostCommandsRejectAnExplicitDomainBeforeHostOrCAAccess(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "work")
	for _, command := range []string{"init", "doctor"} {
		for _, domainArgs := range [][]string{{"--domain", "work"}, {"--domain=work"}, {"--domain="}} {
			t.Run(command+"_"+strings.Join(domainArgs, "_"), func(t *testing.T) {
				host := &hostServiceFake{}
				ca := &caStoreFake{}
				args := append([]string{"--config", configPath}, domainArgs...)
				args = append(args, command)
				err := Run(context.Background(), args, Options{HostInit: host, HostDoctor: host, CAInit: ca, Output: &bytes.Buffer{}})
				if err == nil || !strings.Contains(err.Error(), "--domain") {
					t.Fatalf("Run(%s) error = %v, want explicit-domain refusal", strings.Join(args, " "), err)
				}
				if host.initCalls != 0 || host.doctorCalls != 0 || ca.initCalls != 0 {
					t.Fatalf("rejected %s calls = host init %d doctor %d CA init %d, want none", command, host.initCalls, host.doctorCalls, ca.initCalls)
				}
			})
		}
	}
}

func TestDoctorWritesDeterministicHostFindingsAndReturnsNonzeroForDrift(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "work")
	service := &hostServiceFake{report: hostx.Report{Status: hostx.Drifted, Findings: []hostx.Finding{
		{Code: "z.unsafe", Category: hostx.Drifted, Observed: "z", Expected: "safe", Remedy: "inspect"},
		{Code: "a.mode", Category: hostx.Drifted, Observed: "a", Expected: "04550", Remedy: "inspect"},
	}}}
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "doctor"}, Options{HostDoctor: service, Output: &output})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("Run(doctor) error = %v, want nonzero drift result", err)
	}
	if got, want := output.String(), "status: drifted/unsafe\na.mode: [drifted/unsafe] observed=a expected=04550 remedy=inspect\nz.unsafe: [drifted/unsafe] observed=z expected=safe remedy=inspect\n"; got != want {
		t.Fatalf("doctor output = %q, want %q", got, want)
	}
}

func TestDomainInitInitializesOnlyTheSelectedCAUsingEveryConfiguredDomain(t *testing.T) {
	configPath := writeV2DomainSetFixture(t)
	events := []string{}
	host := &hostServiceFake{events: &events, result: hostx.InitResult{HostInstalled: true, RefreshLoginSession: true}}
	ca := &caStoreFake{events: &events}
	var output bytes.Buffer

	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "domain", "init"}, Options{HostInit: host, HostDoctor: host, CAInit: ca, Output: &output})
	if err != nil {
		t.Fatalf("Run(domain init) error = %v", err)
	}
	if got, want := strings.Join(events, ","), "ca.init"; got != want {
		t.Fatalf("domain init order = %q, want %q", got, want)
	}
	if ca.selected.ID != "work" || len(ca.configured) != 2 || ca.configured[0].ID != "personal" || ca.configured[1].ID != "work" {
		t.Fatalf("CA domains = selected %#v configured %#v, want selected work and complete sorted set", ca.selected, ca.configured)
	}
	if host.initCalls != 0 || host.doctorCalls != 0 {
		t.Fatalf("domain init calls = host init %d doctor %d, want CA init only", host.initCalls, host.doctorCalls)
	}
	if got, want := output.String(), "domain: work\nmanagement-ca: initialized\n"; got != want {
		t.Fatalf("domain init output = %q, want %q", got, want)
	}
}

func TestLegacyDomainCommandsRequireFullV2HostAdmissionBeforeAccess(t *testing.T) {
	for name, write := range map[string]func(*testing.T) string{
		"missing host": func(t *testing.T) string {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "config.json")
			if err := os.WriteFile(path, []byte(fmt.Sprintf(`{"version":2,"domains":{"work":{"state_root":%q}}}`, root)), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
		"unavailable host path": func(t *testing.T) string {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "config.json")
			contents := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q}}}`, filepath.Join(root, "missing-tart"), filepath.Join(root, "missing-home"), filepath.Join(root, "missing-softnet"), root)
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
		"host overlaps domain": func(t *testing.T) string {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			tart := filepath.Join(root, "tart")
			softnet := filepath.Join(root, "softnet")
			home := filepath.Join(root, "tart-home")
			for _, path := range []string{tart, softnet} {
				if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "config.json")
			contents := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q}}}`, tart, home, softnet, root)
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatal(err)
			}
			return path
		},
	} {
		for command, suffix := range map[string][]string{
			"golden register": {"golden", "register", "golden-work-r1"},
			"session create":  {"session", "create", "dev"},
			"session status":  {"session", "status", "dev"},
		} {
			t.Run(name+"_"+command, func(t *testing.T) {
				configPath := write(t)
				observer := &countingObserver{}
				args := append([]string{"--config", configPath, "--domain", "work"}, suffix...)
				err := Run(context.Background(), args, Options{Observer: observer, Output: &bytes.Buffer{}})
				if err == nil || !strings.Contains(err.Error(), "load configuration") {
					t.Fatalf("Run(%s) error = %v, want full host-admission failure", command, err)
				}
				if observer.calls != 0 {
					t.Fatalf("Run(%s) observer calls = %d, want 0 before host admission", command, observer.calls)
				}
			})
		}
	}
}

func TestDomainInitReportsAlreadyInitializedDisposition(t *testing.T) {
	configPath, _ := writeDomainFixture(t, "work")
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "domain", "init"}, Options{
		CAInit: &caStoreFake{result: sshx.CAInitResult{Disposition: sshx.CAAlreadyInitialized}},
		Output: &output,
	})
	if err != nil {
		t.Fatalf("Run(domain init) error = %v", err)
	}
	if got, want := output.String(), "domain: work\nmanagement-ca: already initialized\n"; got != want {
		t.Fatalf("domain init output = %q, want %q", got, want)
	}
}

func TestDomainInitDoesNotRequireHostPrerequisites(t *testing.T) {
	configPath, _ := writeDomainFixture(t, "work")
	host := &hostServiceFake{}
	ca := &caStoreFake{}
	if err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "domain", "init"}, Options{HostInit: host, HostDoctor: host, CAInit: ca, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Run(domain init) error = %v", err)
	}
	if host.initCalls != 0 || host.doctorCalls != 0 || ca.initCalls != 1 {
		t.Fatalf("domain init calls = host init %d doctor %d CA init %d, want CA init only", host.initCalls, host.doctorCalls, ca.initCalls)
	}
}

func TestDomainInitDefersUnavailableV2HostArtifacts(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "work")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	contents := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q}}}`, filepath.Join(base, "missing-tart"), filepath.Join(base, "missing-tart-home"), filepath.Join(base, "missing-softnet"), root)
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	host := &hostServiceFake{}
	ca := &caStoreFake{}
	if err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "domain", "init"}, Options{HostInit: host, HostDoctor: host, CAInit: ca, Output: &bytes.Buffer{}}); err != nil {
		t.Fatalf("Run(domain init) error = %v", err)
	}
	if host.initCalls != 0 || host.doctorCalls != 0 || ca.initCalls != 1 {
		t.Fatalf("domain init calls = host init %d doctor %d CA init %d, want CA init only", host.initCalls, host.doctorCalls, ca.initCalls)
	}
}

func TestDomainInitReportsTheDomainScopedFailureWithoutHostMutation(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "work")
	events := []string{}
	host := &hostServiceFake{events: &events, result: hostx.InitResult{HostInstalled: true}}
	ca := &caStoreFake{events: &events, initErr: errors.New("key validation failed")}
	var output bytes.Buffer

	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "domain", "init"}, Options{HostInit: host, CAInit: ca, Output: &output})
	if err == nil || !strings.Contains(err.Error(), "initialize domain management CA") {
		t.Fatalf("Run(domain init) error = %v, want domain CA failure", err)
	}
	if got, want := strings.Join(events, ","), "ca.init"; got != want {
		t.Fatalf("domain init order = %q, want %q", got, want)
	}
	if output.Len() != 0 {
		t.Fatalf("domain init failure output = %q, want no success output", output.String())
	}
}

func TestDoctorDoesNotReportAnAbsentDomainCA(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "work")
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "doctor"}, Options{
		HostDoctor: &hostServiceFake{},
		Output:     &output,
	})
	if err != nil {
		t.Fatalf("Run(doctor missing CA) error = %v, want host-only success", err)
	}
	if got, want := output.String(), "status: healthy\n"; got != want {
		t.Fatalf("doctor missing-CA output = %q, want %q", got, want)
	}
}

func TestGoldenRegisterPersistsOneObservedStoppedDomainGolden(t *testing.T) {
	configPath, domainConfig := writeDomainFixture(t, "work")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "golden", "register", "golden-work-r1"}, Options{
		Observer: backendFake,
		Output:   &output,
	})
	if err != nil {
		t.Fatalf("Run(golden register) error = %v", err)
	}
	loaded, err := golden.LoadCurrent(context.Background(), domainConfig)
	if err != nil {
		t.Fatalf("LoadCurrent() error = %v", err)
	}
	if loaded.Revision != "golden-work-r1" {
		t.Fatalf("registered revision = %q, want golden-work-r1", loaded.Revision)
	}
	if got, want := output.String(), "domain: work\ngolden: golden-work-r1\nstate: registered\n"; got != want {
		t.Fatalf("Run() output = %q, want %q", got, want)
	}
}

func TestSessionCreateComposesRegisteredGoldenAndCreator(t *testing.T) {
	configPath, domainConfig := writeDomainFixture(t, "work")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
	if err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "golden", "register", "golden-work-r1"}, Options{
		Observer: backendFake,
		Output:   &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("Run(golden register) error = %v", err)
	}

	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "create", "--mode", "quarantine", "dev"}, Options{
		Observer: backendFake,
		Creator:  backendFake,
		Output:   &output,
	})
	if err != nil {
		t.Fatalf("Run(session create) error = %v", err)
	}
	record, err := session.LoadRecord(domainConfig.StateRoot, "work", "dev")
	if err != nil {
		t.Fatalf("LoadRecord() error = %v", err)
	}
	if record.Mode != session.ModeQuarantine || record.IntendedState != session.StateStopped || record.GoldenRevision != "golden-work-r1" {
		t.Fatalf("created record = %#v, want stopped quarantine clone", record)
	}
	if got := len(backendFake.CloneCalls()); got != 1 {
		t.Fatalf("clone calls = %d, want 1", got)
	}
	if got, want := output.String(), "domain: work\nsession: dev\nmode: quarantine\nstate: stopped\n"; got != want {
		t.Fatalf("Run() output = %q, want %q", got, want)
	}
}

func TestSessionCreateRequiresCreatorBeforeMutation(t *testing.T) {
	configPath, _ := writeDomainFixture(t, "work")
	backendFake := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
	if err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "golden", "register", "golden-work-r1"}, Options{
		Observer: backendFake,
		Output:   &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("Run(golden register) error = %v", err)
	}
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "create", "dev"}, Options{
		Observer: backendFake,
		Output:   &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "creator") {
		t.Fatalf("Run(session create) error = %v, want missing creator", err)
	}
	if len(backendFake.CloneCalls()) != 0 || len(backendFake.RandomizeMACCalls()) != 0 {
		t.Fatal("missing creator caused backend mutation")
	}
}

func TestCreateRejectsUnknownModeBeforeBackendAccess(t *testing.T) {
	configPath, _ := writeDomainFixture(t, "work")
	observer := &countingObserver{}
	err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "create", "--mode", "durable", "dev"}, Options{
		Observer: observer,
		Creator:  fake.New(),
		Output:   &bytes.Buffer{},
	})
	if err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("Run() error = %v, want invalid mode", err)
	}
	if observer.calls != 0 {
		t.Fatalf("observer calls = %d, want 0", observer.calls)
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

func writeDomainFixture(t *testing.T, rawDomain string) (string, config.Domain) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.json")
	contents := `{"version":1,"domains":{"` + rawDomain + `":{"state_root":"` + root + `"}}}`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	domainConfig, err := loaded.Domain(rawDomain)
	if err != nil {
		t.Fatal(err)
	}
	return configPath, domainConfig
}

func writeV2DomainFixture(t *testing.T, rawDomain string) (string, config.Domain) {
	t.Helper()
	path, selected := writeDomainFixture(t, rawDomain)
	hostBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tartExecutable := filepath.Join(hostBase, "tart")
	softnetSource := filepath.Join(hostBase, "softnet")
	tartHome := filepath.Join(hostBase, "tart-home")
	if err := os.WriteFile(tartExecutable, []byte("tart fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(softnetSource, []byte("softnet fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tartHome, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := []byte(fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"%s":{"state_root":%q}}}`, tartExecutable, tartHome, softnetSource, rawDomain, selected.StateRoot))
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, selected
}

func writeV2DomainSetFixture(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(base, "work")
	personal := filepath.Join(base, "personal")
	for _, directory := range []string{work, personal} {
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	hostBase, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tartExecutable := filepath.Join(hostBase, "tart")
	softnetSource := filepath.Join(hostBase, "softnet")
	tartHome := filepath.Join(hostBase, "tart-home")
	if err := os.WriteFile(tartExecutable, []byte("tart fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(softnetSource, []byte("softnet fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tartHome, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(base, "config.json")
	contents := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`, tartExecutable, tartHome, softnetSource, work, personal)
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return configPath
}

type hostServiceFake struct {
	initCalls   int
	doctorCalls int
	report      hostx.Report
	result      hostx.InitResult
	events      *[]string
	request     hostx.Request
}

func (f *hostServiceFake) Init(_ context.Context, request hostx.Request) (hostx.InitResult, error) {
	f.initCalls++
	f.request = request
	if f.events != nil {
		*f.events = append(*f.events, "host.init")
	}
	if f.result != (hostx.InitResult{}) {
		return f.result, nil
	}
	return hostx.InitResult{HostInstalled: true}, nil
}

func (f *hostServiceFake) Doctor(_ context.Context, request hostx.Request) hostx.Report {
	f.doctorCalls++
	f.request = request
	if f.events != nil {
		*f.events = append(*f.events, "host.doctor")
	}
	if f.report.Status != "" {
		return f.report
	}
	return hostx.Report{Status: hostx.Healthy}
}

type caStoreFake struct {
	initCalls  int
	initErr    error
	result     sshx.CAInitResult
	events     *[]string
	selected   sshx.Domain
	configured []sshx.Domain
}

func (f *caStoreFake) Init(_ context.Context, selected sshx.Domain, configured []sshx.Domain) (sshx.CAInitResult, error) {
	f.initCalls++
	if f.events != nil {
		*f.events = append(*f.events, "ca.init")
	}
	f.selected = selected
	f.configured = append([]sshx.Domain(nil), configured...)
	if f.result != (sshx.CAInitResult{}) {
		return f.result, f.initErr
	}
	return sshx.CAInitResult{Disposition: sshx.CAInitialized}, f.initErr
}
