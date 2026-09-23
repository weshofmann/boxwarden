package basebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

type noExtendedACL struct{}

func (noExtendedACL) HasExtendedACL(string) (bool, error) { return false, nil }

func usePortableACLFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		return
	}
	previous := basebuildACLInspector
	basebuildACLInspector = noExtendedACL{}
	t.Cleanup(func() { basebuildACLInspector = previous })
}

func TestAttemptWriteFailureLeavesCommittedPhaseAndPermitsFailureJournal(t *testing.T) {
	usePortableACLFixture(t)
	dir := t.TempDir()
	var err error
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	before := Attempt{Version: 1, CandidateID: "candidate", PreparationKey: strings.Repeat("a", 64), Phase: PhaseRunning}
	if err := writeAttempt(dir, before); err != nil {
		t.Fatal(err)
	}
	want := errors.New("injected write failure after temporary creation")
	if err := writeAttemptWithHook(dir, Attempt{Version: 1, CandidateID: before.CandidateID, PreparationKey: before.PreparationKey, Phase: PhaseFinalizing}, func() error { return want }); !errors.Is(err, want) {
		t.Fatalf("interrupted write error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "attempt.json.next")); !os.IsNotExist(err) {
		t.Fatalf("temporary journal left after failure: %v", err)
	}
	stored, err := ReadAttempt(dir)
	if err != nil || stored != before {
		t.Fatalf("committed journal changed = %#v, %v", stored, err)
	}
	after := before
	after.Phase = PhaseFailed
	if err := writeAttempt(dir, after); err != nil {
		t.Fatalf("failure journal blocked by prior temporary: %v", err)
	}
}

func TestFreshRunIDContract(t *testing.T) {
	for _, valid := range []string{"run-1", "run-2", "run-0123456789ab"} {
		if !validRunID(valid) {
			t.Fatalf("valid run ID rejected: %q", valid)
		}
	}
	for _, invalid := range []string{"run-3", "run-0123456789AB", "run-0123456789a", "run-0123456789abc", "run-0123456789ab/"} {
		if validRunID(invalid) {
			t.Fatalf("invalid run ID accepted: %q", invalid)
		}
	}
	if _, err := RenderCommand(RenderRequest{GuestDefinitionRoot: "/private/guest", RunID: "run-0123456789ab", VerifierFile: "/private/hash", OutputDirectory: "/private/seed"}); err != nil {
		t.Fatalf("fresh run ID could not render: %v", err)
	}
}

type fakeChecks struct {
	called int
	staged Inputs
}

func (f *fakeChecks) Verify(Inputs) (string, error) {
	f.called++
	return strings.Repeat("a", 64), nil
}

func (f *fakeChecks) Stage(_ context.Context, in Inputs, _ string, _ string) (Inputs, error) {
	if f.staged.ISOPath != "" {
		return f.staged, nil
	}
	return in, nil
}

type fakeSeed struct {
	events      *[]string
	renderReq   *RenderRequest
	remasterReq *RemasterRequest
}

func (f fakeSeed) BuilderVerifier(_ context.Context, attemptDir string) (string, error) {
	*f.events = append(*f.events, "verifier")
	path := filepath.Join(attemptDir, "builder-verifier")
	if err := os.WriteFile(path, []byte("$6$fake"), 0600); err != nil {
		return "", err
	}
	return path, nil
}
func (f fakeSeed) Render(_ context.Context, req RenderRequest) error {
	*f.events = append(*f.events, "render")
	if f.renderReq != nil {
		*f.renderReq = req
	}
	return nil
}
func (f fakeSeed) Remaster(_ context.Context, req RemasterRequest) error {
	*f.events = append(*f.events, "remaster")
	if f.remasterReq != nil {
		*f.remasterReq = req
	}
	return nil
}

func TestBuildPassesStagedSourcesToSeedBuilder(t *testing.T) {
	usePortableACLFixture(t)
	in := exampleInputs(t)
	events := []string{}
	staged := in
	staged.ISOPath = filepath.Join(in.AttemptRoot, in.AttemptID, "source.iso")
	staged.GuestDefinitionRoot = filepath.Join(in.AttemptRoot, in.AttemptID, "guest-definition")
	checks := &fakeChecks{staged: staged}
	var render RenderRequest
	var remaster RemasterRequest
	seed := fakeSeed{events: &events, renderReq: &render, remasterReq: &remaster}
	vm := &fakeVM{events: &events, run: &fakeRun{events: &events}}
	if _, err := Build(context.Background(), in, Dependencies{Checks: checks, Seed: seed, VM: vm}); err != nil {
		t.Fatal(err)
	}
	if render.GuestDefinitionRoot != staged.GuestDefinitionRoot || remaster.GuestDefinitionRoot != staged.GuestDefinitionRoot || remaster.SourceISO != staged.ISOPath {
		t.Fatalf("builder consumed original source paths: render=%+v remaster=%+v", render, remaster)
	}
}

func TestBuildPassesPrivateGuestPreparationPayloadToRemaster(t *testing.T) {
	in := exampleInputs(t)
	in.Recipe.AptPackages = []string{"git"}
	in.Recipe.Steps = []recipe.Step{{ID: "base-step", Phase: "prepare", Argv: []string{"/bin/echo", "literal arg"}}}
	events := []string{}
	var remaster RemasterRequest
	seed := fakeSeed{events: &events, remasterReq: &remaster}
	vm := &fakeVM{events: &events, run: &fakeRun{events: &events}}
	if _, err := Build(context.Background(), in, Dependencies{Checks: &fakeChecks{}, Seed: seed, VM: vm}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(in.AttemptRoot, in.AttemptID, "recipe-prepare.json")
	if remaster.PreparationJSON != path {
		t.Fatalf("remaster preparation path = %q, want %q", remaster.PreparationJSON, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"version":1,"preparation_key":"` + strings.Repeat("a", 64) + `","apt_packages":["git"],"steps":[{"id":"base-step","argv":["/bin/echo","literal arg"]}]}`
	if string(raw) != want {
		t.Fatalf("guest preparation payload = %s", raw)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Fatalf("guest preparation payload is not private: %v, %v", info, err)
	}
}

type fakeVM struct {
	events   *[]string
	state    backend.ObjectState
	exists   bool
	run      *fakeRun
	failAt   string
	identity string
}

func (f *fakeVM) CandidateIdentity(_ context.Context, _ string) (string, error) {
	if f.identity == "" {
		return strings.Repeat("b", 64), nil
	}
	return f.identity, nil
}

func (f *fakeVM) Observe(_ context.Context, id string) (backend.Observation, error) {
	*f.events = append(*f.events, "observe")
	if f.run != nil && f.run.waited {
		f.state = backend.ObjectStopped
	}
	return backend.Observation{ObjectID: id, Exists: f.exists, State: f.state}, nil
}
func (f *fakeVM) Create(_ context.Context, request CreateRequest) error {
	*f.events = append(*f.events, "create")
	if f.failAt == "create" {
		return errors.New("create failed")
	}
	f.exists = true
	f.state = backend.ObjectStopped
	return nil
}
func (f *fakeVM) Configure(_ context.Context, request ConfigureRequest) error {
	*f.events = append(*f.events, "configure")
	return nil
}
func (f *fakeVM) RunInstaller(_ context.Context, request InstallerRequest) (InstallerHandle, error) {
	*f.events = append(*f.events, "run")
	f.state = backend.ObjectRunning
	if f.run == nil {
		return nil, errors.New("missing fake run")
	}
	return f.run, nil
}

type fakeRun struct {
	events  *[]string
	failAt  string
	stopped bool
	waited  bool
}

func (f *fakeRun) WaitFor(_ context.Context, marker string) error {
	switch marker {
	case InstalledPrompt("run-1"):
		*f.events = append(*f.events, "installed")
	case CloneReadyMarker:
		*f.events = append(*f.events, "finalized")
	default:
		return errors.New("unexpected marker")
	}
	if f.failAt == "finalized" && marker == CloneReadyMarker {
		return errors.New("finalizer failed")
	}
	return nil
}
func (f *fakeRun) SendLine(_ context.Context, line string) error {
	switch line {
	case FinalizerCommand:
		*f.events = append(*f.events, "finalizer-command")
	case PoweroffCommand:
		*f.events = append(*f.events, "poweroff-command")
	default:
		return errors.New("unexpected command")
	}
	return nil
}
func (f *fakeRun) Stop(context.Context) error {
	*f.events = append(*f.events, "stop")
	f.stopped = true
	return nil
}
func (f *fakeRun) Wait(context.Context) error {
	*f.events = append(*f.events, "wait")
	f.waited = true
	return nil
}

func exampleInputs(t *testing.T) Inputs {
	t.Helper()
	usePortableACLFixture(t)
	root := t.TempDir()
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0700); err != nil {
		t.Fatal(err)
	}
	return Inputs{AttemptRoot: root, AttemptID: "attempt-1", CandidateID: "bw-v02-a1444-build-r1", RunID: "run-1", ISOPath: "/private/ubuntu.iso", GuestDefinitionRoot: "/private/guest", Recipe: recipe.Recipe{Version: 1, Source: recipe.Source{Kind: "ubuntu-24.04.4-desktop-arm64", SHA256: "c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"}, Machine: recipe.Machine{CPUs: 4, MemoryMiB: 4096, SystemDiskGiB: 40}}}
}

func TestBuildPreservesStoppedCandidateWithoutClaimingQualification(t *testing.T) {
	inputs := exampleInputs(t)
	events := []string{}
	checks := &fakeChecks{}
	run := &fakeRun{events: &events}
	vm := &fakeVM{events: &events, run: run}
	result, err := Build(context.Background(), inputs, Dependencies{Checks: checks, Seed: fakeSeed{events: &events}, VM: vm})
	if err != nil {
		t.Fatal(err)
	}
	if result.State != CandidateStopped || result.Qualified || result.CacheAdmitted {
		t.Fatalf("unsafe result: %+v", result)
	}
	if checks.called != 1 {
		t.Fatalf("verification calls = %d", checks.called)
	}
	want := "observe,verifier,render,remaster,create,observe,configure,run,installed,finalizer-command,finalized,poweroff-command,wait,observe"
	if got := strings.Join(events, ","); got != want {
		t.Fatalf("events: %s\nwant: %s", got, want)
	}
	if run.stopped {
		t.Fatal("successful shutdown was force-stopped")
	}
	if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID, "builder-verifier")); !os.IsNotExist(err) {
		t.Fatalf("temporary verifier retained: %v", err)
	}
	state, err := ReadAttempt(filepath.Join(inputs.AttemptRoot, inputs.AttemptID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseCandidateStopped || state.CandidateID != inputs.CandidateID {
		t.Fatalf("state: %+v", state)
	}
}

func TestBuildFailurePreservesEvidenceAndReapsOwnedProcess(t *testing.T) {
	inputs := exampleInputs(t)
	events := []string{}
	run := &fakeRun{events: &events, failAt: "finalized"}
	vm := &fakeVM{events: &events, run: run}
	_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{events: &events}, VM: vm})
	if err == nil || !strings.Contains(err.Error(), "finalizer failed") {
		t.Fatalf("error = %v", err)
	}
	if !run.stopped {
		t.Fatal("owned process was not stopped")
	}
	state, err := ReadAttempt(filepath.Join(inputs.AttemptRoot, inputs.AttemptID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseFailed || state.CandidateID != inputs.CandidateID {
		t.Fatalf("state: %+v", state)
	}
	if state.Failure != "build failed during finalizing" {
		t.Fatalf("failure phase: %q", state.Failure)
	}
	if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID, "builder-verifier")); !os.IsNotExist(err) {
		t.Fatalf("failed attempt retained verifier: %v", err)
	}
	if _, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{events: &events}, VM: vm}); err == nil {
		t.Fatal("failed attempt was reused")
	}
}

type unsafeVerifierSeed struct {
	fakeSeed
	mode     os.FileMode
	hardLink string
}

func (f unsafeVerifierSeed) BuilderVerifier(_ context.Context, attemptDir string) (string, error) {
	path := filepath.Join(attemptDir, "builder-verifier")
	if err := os.WriteFile(path, []byte("$6$fake"), f.mode); err != nil {
		return "", err
	}
	if f.hardLink != "" {
		if err := os.Link(path, f.hardLink); err != nil {
			return "", err
		}
	}
	return path, nil
}

func TestBuildRemovesUnsafeReturnedVerifier(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mode     os.FileMode
		hardLink bool
	}{{"world-readable", 0644, false}, {"multiply-linked", 0600, true}} {
		t.Run(tc.name, func(t *testing.T) {
			inputs := exampleInputs(t)
			events := []string{}
			seed := unsafeVerifierSeed{fakeSeed: fakeSeed{events: &events}, mode: tc.mode}
			if tc.hardLink {
				seed.hardLink = filepath.Join(t.TempDir(), "extra-link")
			}
			_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: seed, VM: &fakeVM{events: &events}})
			if err == nil {
				t.Fatal("unsafe verifier accepted")
			}
			if !strings.Contains(err.Error(), "builder verifier") {
				t.Fatalf("wrong error: %v", err)
			}
			if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID, "builder-verifier")); !os.IsNotExist(err) {
				t.Fatalf("unsafe verifier retained in attempt: %v", err)
			}
		})
	}
}

func TestBuildRejectsOccupiedCandidateBeforeStaging(t *testing.T) {
	inputs := exampleInputs(t)
	events := []string{}
	vm := &fakeVM{events: &events, exists: true, state: backend.ObjectStopped}
	_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{events: &events}, VM: vm})
	if err == nil {
		t.Fatal("occupied candidate accepted")
	}
	if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID)); !os.IsNotExist(err) {
		t.Fatalf("staging created: %v", err)
	}
}

func TestBuildRequiresExplicitBuilderVerifierProvider(t *testing.T) {
	inputs := exampleInputs(t)
	_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, VM: &fakeVM{}})
	if err == nil || !strings.Contains(err.Error(), "seed builder") {
		t.Fatalf("error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(inputs.AttemptRoot, inputs.AttemptID)); !os.IsNotExist(err) {
		t.Fatalf("staging created: %v", err)
	}
}

func TestBuildRecordsCreateFailureWithoutStartingGuest(t *testing.T) {
	inputs := exampleInputs(t)
	events := []string{}
	vm := &fakeVM{events: &events, failAt: "create"}
	_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{events: &events}, VM: vm})
	if err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("error = %v", err)
	}
	state, readErr := ReadAttempt(filepath.Join(inputs.AttemptRoot, inputs.AttemptID))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if state.Phase != PhaseFailed {
		t.Fatalf("state: %+v", state)
	}
	if strings.Contains(strings.Join(events, ","), "run") {
		t.Fatalf("ran failed candidate: %v", events)
	}
}

func TestBuilderCommandsKeepArgumentsSeparate(t *testing.T) {
	render, err := RenderCommand(RenderRequest{GuestDefinitionRoot: "/trusted/guest", RunID: "run-1", VerifierFile: "/private/attempt/hash", OutputDirectory: "/private/attempt/seed"})
	if err != nil {
		t.Fatal(err)
	}
	if render.Path != "/trusted/guest/render-golden-seed.sh" || strings.Join(render.Args, "|") != "run-1|/private/attempt/hash|/private/attempt/seed" {
		t.Fatalf("render command: %+v", render)
	}
	remaster, err := RemasterCommand(RemasterRequest{GuestDefinitionRoot: "/trusted/guest", SourceISO: "/private/ubuntu.iso", RenderedUserData: "/private/attempt/seed/user-data", PreparationJSON: "/private/attempt/recipe-prepare.json", OutputISO: "/private/attempt/installer.iso"})
	if err != nil {
		t.Fatal(err)
	}
	if remaster.Path != "/trusted/guest/remaster-golden-iso.sh" || strings.Join(remaster.Args, "|") != "/private/ubuntu.iso|/private/attempt/seed/user-data|/private/attempt/recipe-prepare.json|/private/attempt/installer.iso" {
		t.Fatalf("remaster command: %+v", remaster)
	}
	if _, err := RenderCommand(RenderRequest{GuestDefinitionRoot: "/trusted/guest", RunID: "run-3", VerifierFile: "/private/attempt/hash", OutputDirectory: "/private/attempt/seed"}); err == nil {
		t.Fatal("unsupported run ID accepted")
	}
}

func TestBuildRejectsSymlinkedAttemptAncestor(t *testing.T) {
	inputs := exampleInputs(t)
	outer := t.TempDir()
	if err := os.Mkdir(filepath.Join(inputs.AttemptRoot, "attempts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(inputs.AttemptRoot, filepath.Join(outer, "link")); err != nil {
		t.Fatal(err)
	}
	inputs.AttemptRoot = filepath.Join(outer, "link", "attempts")
	_, err := Build(context.Background(), inputs, Dependencies{Checks: &fakeChecks{}, Seed: fakeSeed{}, VM: &fakeVM{}})
	if err == nil {
		t.Fatal("symlinked attempt root accepted")
	}
}
