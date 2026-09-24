package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/golden"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspaceformat"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type appFormatFunc func(context.Context, workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error)

func (f appFormatFunc) FormatAndVerify(ctx context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
	return f(ctx, request)
}

func TestWorkspaceAttachAndDetachUseExactStoppedSessionAndQualifiedVolume(t *testing.T) {
	configPath, selected := writeDomainFixture(t, "work")
	observer := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
	options := Options{Observer: observer, Creator: observer, Output: &bytes.Buffer{}}
	for _, suffix := range [][]string{{"golden", "register", "golden-work-r1"}, {"session", "create", "dev"}} {
		if err := Run(t.Context(), append([]string{"--config", configPath, "--domain", "work"}, suffix...), options); err != nil {
			t.Fatal(err)
		}
	}
	const volumeID = "00112233-4455-4677-8899-aabbccddeeff"
	const fsUUID = "10213243-5465-4768-899a-bbccddeeff00"
	held, err := workspacex.AcquireStorageOperation(t.Context(), selected.StateRoot, domain.ID("work"))
	if err != nil {
		t.Fatal(err)
	}
	err = workspacex.SaveRecord(selected.StateRoot, domain.ID("work"), workspacex.Record{Version: 1, Domain: "work", VolumeID: volumeID,
		SizeBytes: 64 << 20, Format: workspacex.FormatRawExt4, FilesystemUUID: fsUUID, State: workspacex.StateCreating})
	if releaseErr := held.Release(); err != nil || releaseErr != nil {
		t.Fatalf("save creating record: %v; release: %v", err, releaseErr)
	}
	_, err = workspaceformat.Create(t.Context(), selected.StateRoot, workspaceformat.Request{Domain: "work", VolumeID: volumeID, FilesystemUUID: fsUUID, SizeBytes: 64 << 20},
		appFormatFunc(func(_ context.Context, request workspaceformat.FormatRequest) (workspaceformat.FormatEvidence, error) {
			raw, err := os.OpenFile(request.DiskPath, os.O_WRONLY, 0)
			if err != nil {
				return workspaceformat.FormatEvidence{}, err
			}
			defer raw.Close()
			superblock := make([]byte, 1024)
			superblock[0x38], superblock[0x39] = 0x53, 0xef
			decoded, err := hex.DecodeString(strings.ReplaceAll(fsUUID, "-", ""))
			if err != nil {
				return workspaceformat.FormatEvidence{}, err
			}
			copy(superblock[0x68:], decoded)
			if _, err := raw.WriteAt(superblock, 1024); err != nil {
				return workspaceformat.FormatEvidence{}, err
			}
			return workspaceformat.FormatEvidence{ObservedUUID: fsUUID, WholeDevice: true, FilesystemClean: true}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspacex.PromoteVerified(t.Context(), selected.StateRoot, domain.ID("work"), volumeID); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	options.Output = &output
	args := []string{"--config", configPath, "--domain", "work", "workspace", "attach", "--mount", "/home/boxwarden/workspaces/project", volumeID, "dev"}
	if err := Run(t.Context(), args, options); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "workspace: attached") {
		t.Fatalf("attach output: %q", output.String())
	}
	attached, err := workspacex.LoadRecord(selected.StateRoot, domain.ID("work"), volumeID)
	if err != nil || attached.Attachment == nil || attached.Attachment.SessionName != "dev" {
		t.Fatalf("attachment: %+v, %v", attached, err)
	}
	if err := Run(t.Context(), args, options); err == nil {
		t.Fatal("already attached workspace accepted twice")
	}
	output.Reset()
	if err := Run(t.Context(), []string{"--config", configPath, "--domain", "work", "workspace", "detach", volumeID, "dev"}, options); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "workspace: detached") {
		t.Fatalf("detach output: %q", output.String())
	}
	detached, err := workspacex.LoadRecord(selected.StateRoot, domain.ID("work"), volumeID)
	if err != nil || detached.Attachment != nil || detached.Disk == nil || *detached.Disk != *attached.Disk {
		t.Fatalf("detached volume lost disk identity: %+v, %v", detached, err)
	}
}

func TestWorkspaceExportRoutesExactDomainSelectionAndInputs(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	observer := fake.New()
	const volumeID = "00112233-4455-4677-8899-aabbccddeeff"
	const transaction = "10213243-5465-4768-899a-bbccddeeff00"
	destination := filepath.Join(t.TempDir(), "returned")
	input := AlphaExportInput{VolumeID: volumeID, DestinationParent: destination,
		Selected: []string{"project/report.txt", "notes"}, SourceRoot: "/private/clean-source",
		ISOPath: "/private/ubuntu.iso", GoBinary: "/private/bin/go"}
	called := 0
	var output bytes.Buffer
	options := Options{Observer: observer, Output: &output,
		AlphaExport: func(_ context.Context, actual config.Domain, received AlphaExportInput, actualObserver backend.Observer) (workspacex.ExportJournal, string, error) {
			called++
			if actual != selected || actualObserver != observer || !reflect.DeepEqual(received, input) {
				return workspacex.ExportJournal{}, "", fmt.Errorf("export lost selected domain or inputs")
			}
			return workspacex.ExportJournal{ID: transaction, Domain: actual.ID,
					DestinationParent: destination, Phase: workspacex.ExportPublished},
				filepath.Join(destination, strings.ReplaceAll(transaction, "-", "")), nil
		}}
	args := []string{"--config", configPath, "--domain", "alpha", "workspace", "export",
		"--destination", destination, "--select", "project/report.txt", "--select", "notes",
		"--source-root", input.SourceRoot, "--iso", input.ISOPath, "--go", input.GoBinary, volumeID}
	if err := Run(t.Context(), args, options); err != nil || called != 1 || !strings.Contains(output.String(), "transaction: "+transaction+"\n") {
		t.Fatalf("public export routing = called %d, output %q, error %v", called, output.String(), err)
	}
	for _, invalid := range [][]string{
		{"--destination", destination, "--source-root", input.SourceRoot, "--iso", input.ISOPath, "--go", input.GoBinary, volumeID},
		{"--destination", "relative", "--select", "project/report.txt", "--source-root", input.SourceRoot, "--iso", input.ISOPath, "--go", input.GoBinary, volumeID},
	} {
		if err := Run(t.Context(), append([]string{"--config", configPath, "--domain", "alpha", "workspace", "export"}, invalid...), options); err == nil {
			t.Fatalf("invalid public export input accepted: %v", invalid)
		}
	}
	if called != 1 {
		t.Fatalf("invalid input invoked exporter %d times", called)
	}
	workConfig, _ := writeV2DomainFixture(t, "work")
	workArgs := append([]string{"--config", workConfig, "--domain", "work", "workspace", "export"}, args[6:]...)
	if err := Run(t.Context(), workArgs, options); err == nil || called != 1 {
		t.Fatalf("non-alpha domain used v0.2 exporter: called %d, error %v", called, err)
	}
}

func TestBackendFactoryBindsRegisterCreateAndStatusToAdmittedConfigAndDomain(t *testing.T) {
	path := writeV2DomainSetFixture(t)
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := loaded.Domain("work")
	admitted := fake.New(backend.Observation{ObjectID: "golden-work-r1", Exists: true, State: backend.ObjectStopped})
	directObserver, directCreator := &countingObserver{}, fake.New()
	for _, suffix := range [][]string{{"golden", "register", "golden-work-r1"}, {"session", "create", "dev"}, {"session", "status", "dev"}} {
		t.Run(strings.Join(suffix, "_"), func(t *testing.T) {
			calls := 0
			var output bytes.Buffer
			err := Run(context.Background(), append([]string{"--config", path, "--domain", "work"}, suffix...), Options{
				ConfigPath: "/unused/config.json", Output: &output,
				Observer: directObserver, Creator: directCreator,
				BackendFactory: func(got config.Config, domain config.Domain) (BackendDependencies, error) {
					calls++
					if !reflect.DeepEqual(got, loaded) || domain != selected {
						t.Fatalf("factory inputs = %#v, %#v; want exact loaded config and work domain", got, domain)
					}
					deps := BackendDependencies{Observer: admitted}
					if suffix[1] == "create" {
						deps.Creator = admitted
					}
					return deps, nil
				},
			})
			if err != nil || calls != 1 || output.Len() == 0 {
				t.Fatalf("Run = %v, factory calls %d, output %q", err, calls, output.String())
			}
			if directObserver.calls != 0 || len(directCreator.CloneCalls()) != 0 || len(directCreator.RandomizeMACCalls()) != 0 {
				t.Fatal("factory composition used directly injected backend")
			}
		})
	}
	record, err := session.LoadRecord(selected.StateRoot, "work", "dev")
	if err != nil || record.IntendedState != session.StateStopped || record.GoldenRevision != "golden-work-r1" {
		t.Fatalf("record = %#v, %v", record, err)
	}
	if calls := admitted.CloneCalls(); len(calls) != 1 || calls[0].SourceID != "golden-work-r1" || calls[0].TargetID != record.Backend.ObjectID {
		t.Fatalf("clones = %#v, want single exact recorded identity", calls)
	}
	if calls := admitted.RandomizeMACCalls(); !reflect.DeepEqual(calls, []string{record.Backend.ObjectID}) {
		t.Fatalf("MAC calls = %#v", calls)
	}
}

func TestAlphaRecipeCheckRequiresExactDomainAndInstaller(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "alpha")
	recipePath := filepath.Join(t.TempDir(), "recipe.json")
	recipeData := `{"version":1,"source":{"kind":"ubuntu-24.04.4-desktop-arm64","sha256":"c2610520bf582976839a1724c669e1cfed0547427be5a0ad12d457b92b46ffbe"},"machine":{"cpus":4,"memory_mib":4096,"system_disk_gib":30}}`
	if err := os.WriteFile(recipePath, []byte(recipeData), 0o600); err != nil {
		t.Fatal(err)
	}
	isoPath := filepath.Join(t.TempDir(), "wrong.iso")
	if err := os.WriteFile(isoPath, []byte("wrong"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, prefix := range map[string][]string{
		"missing-domain": {"--config", configPath},
		"unknown-domain": {"--config", configPath, "--domain", "other"},
		"wrong-iso":      {"--config", configPath, "--domain", "alpha"},
	} {
		t.Run(name, func(t *testing.T) {
			var output bytes.Buffer
			args := append(append([]string(nil), prefix...), "alpha", "recipe", "check", "--recipe", recipePath, "--iso", isoPath)
			if err := Run(context.Background(), args, Options{Output: &output}); err == nil || output.Len() != 0 {
				t.Fatalf("unverified recipe succeeded or emitted success: err=%v output=%q", err, output.String())
			}
		})
	}
	parsed, err := parseCommand([]string{"--config", configPath, "--domain", "alpha", "alpha", "recipe", "check", "--recipe", recipePath, "--iso", isoPath}, Options{})
	if err != nil || parsed.kind != commandAlphaRecipeCheck || parsed.domain != "alpha" || parsed.recipePath != recipePath || parsed.isoPath != isoPath {
		t.Fatalf("alpha recipe command = %+v, %v", parsed, err)
	}
}

func TestAlphaPrepareRoutesExactDomainAndReportsOnlyPassingCacheReceipt(t *testing.T) {
	configPath, _ := writeV2DomainFixture(t, "alpha")
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := loaded.Domain("alpha")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := AlphaPrepareInput{RecipePath: filepath.Join(root, "recipe.json"), ISOPath: filepath.Join(root, "ubuntu.iso"), GuestDefinitionRoot: filepath.Join(root, "guest"), OpenSSLPath: "/usr/bin/openssl", OpenSSLSHA256: strings.Repeat("a", 64), XorrisoPath: "/opt/homebrew/bin/xorriso", XorrisoSHA256: strings.Repeat("b", 64)}
	args := []string{"--config", configPath, "--domain", "alpha", "alpha", "prepare", "--recipe", input.RecipePath, "--iso", input.ISOPath, "--guest-definition", input.GuestDefinitionRoot, "--openssl", input.OpenSSLPath, "--openssl-sha256", input.OpenSSLSHA256, "--xorriso", input.XorrisoPath, "--xorriso-sha256", input.XorrisoSHA256}
	called := 0
	result := basebuild.PreparedResult{Disposition: basebuild.PreparedBuilt, Record: basebuild.PreparedRecord{Version: 2, CandidateID: "boxwarden-alpha-base-abc", CandidateIdentity: strings.Repeat("c", 64), PreparationKey: strings.Repeat("d", 64), AttemptDirectory: filepath.Join(selected.StateRoot, "prepared-attempts", "alpha-attempt-abc"), Qualification: basebuild.QualificationReceipt{CandidateID: "boxwarden-alpha-base-abc", PreparationKey: strings.Repeat("d", 64), CloneID: "boxwarden-alpha-clone-abc", EvidenceSHA256: strings.Repeat("e", 64), BOMSHA256: strings.Repeat("f", 64), Passed: true}}}
	preparer := func(_ context.Context, loaded config.Config, selected config.Domain, path string, got AlphaPrepareInput) (basebuild.PreparedResult, error) {
		called++
		admitted, err := loaded.Domain("alpha")
		if err != nil || selected != admitted || path != configPath || got != input {
			t.Fatalf("preparer binding = %+v, %q, %+v, %v", selected, path, got, err)
		}
		return result, nil
	}
	var output bytes.Buffer
	if err := Run(context.Background(), args, Options{Output: &output, AlphaPrepare: preparer}); err != nil || called != 1 || !strings.Contains(output.String(), "prepared-base: boxwarden-alpha-base-abc") {
		t.Fatalf("prepare result = %v, calls=%d, output=%q", err, called, output.String())
	}
	output.Reset()
	result.Record.Qualification.Passed = false
	if err := Run(context.Background(), args, Options{Output: &output, AlphaPrepare: preparer}); err == nil || output.Len() != 0 {
		t.Fatalf("invalid cache receipt emitted success: %v, %q", err, output.String())
	}
	output.Reset()
	withoutDomain := append([]string{"--config", configPath}, args[4:]...)
	if err := Run(context.Background(), withoutDomain, Options{Output: &output, AlphaPrepare: preparer}); err == nil || called != 2 {
		t.Fatalf("missing domain reached preparer: %v, calls=%d", err, called)
	}
	for _, invalid := range [][]string{
		args[:len(args)-2],
		append(append([]string(nil), args...), "unexpected"),
		func() []string { bad := append([]string(nil), args...); bad[11] = "relative/guest"; return bad }(),
		func() []string {
			bad := append([]string(nil), args...)
			bad[len(bad)-1] = strings.Repeat("A", 64)
			return bad
		}(),
	} {
		output.Reset()
		if err := Run(context.Background(), invalid, Options{Output: &output, AlphaPrepare: preparer}); err == nil || output.Len() != 0 || called != 2 {
			t.Fatalf("invalid command reached preparer: %v, output=%q calls=%d", err, output.String(), called)
		}
	}
}

func TestRecipeSessionCreateUsesPreparedRevisionWithoutChangingCurrent(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	legacy := "golden-alpha-legacy"
	prepared := "boxwarden-alpha-base-prepared"
	vm := fake.New(
		backend.Observation{ObjectID: legacy, Exists: true, State: backend.ObjectStopped},
		backend.Observation{ObjectID: prepared, Exists: true, State: backend.ObjectStopped},
	)
	if _, err := golden.Register(t.Context(), selected, legacy, vm); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	input := AlphaPrepareInput{RecipePath: filepath.Join(root, "recipe.json"), ISOPath: filepath.Join(root, "ubuntu.iso"), GuestDefinitionRoot: filepath.Join(root, "guest"), OpenSSLPath: "/usr/bin/openssl", OpenSSLSHA256: strings.Repeat("a", 64), XorrisoPath: "/usr/bin/xorriso", XorrisoSHA256: strings.Repeat("b", 64)}
	args := []string{"--config", configPath, "--domain", "alpha", "session", "create", "--mode", "clean", "--recipe", input.RecipePath, "--iso", input.ISOPath, "--guest-definition", input.GuestDefinitionRoot, "--openssl", input.OpenSSLPath, "--openssl-sha256", input.OpenSSLSHA256, "--xorriso", input.XorrisoPath, "--xorriso-sha256", input.XorrisoSHA256, "dev"}
	result := basebuild.PreparedResult{Disposition: basebuild.PreparedReused, Record: basebuild.PreparedRecord{Version: 2, CandidateID: prepared, CandidateIdentity: strings.Repeat("c", 64), PreparationKey: strings.Repeat("d", 64), AttemptDirectory: filepath.Join(selected.StateRoot, "prepared-attempts", "attempt-1"), Qualification: basebuild.QualificationReceipt{CandidateID: prepared, PreparationKey: strings.Repeat("d", 64), CloneID: "boxwarden-alpha-qualified-clone", EvidenceSHA256: strings.Repeat("e", 64), BOMSHA256: strings.Repeat("f", 64), Passed: true}}}
	calls := 0
	preparer := func(_ context.Context, loaded config.Config, domain config.Domain, path string, got AlphaPrepareInput) (basebuild.PreparedResult, error) {
		calls++
		admitted, err := loaded.Domain("alpha")
		if err != nil || domain != admitted || path != configPath || got != input {
			t.Fatalf("preparer binding: %+v %q %+v %v", domain, path, got, err)
		}
		if _, err := golden.RegisterRevision(t.Context(), selected, prepared, vm); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
	var output bytes.Buffer
	options := Options{Observer: vm, Creator: vm, AlphaPrepare: preparer, Output: &output}
	if err := Run(t.Context(), args, options); err != nil {
		t.Fatal(err)
	}
	record, err := session.LoadRecord(selected.StateRoot, "alpha", "dev")
	if err != nil || record.GoldenRevision != prepared || record.IntendedState != session.StateStopped || calls != 1 {
		t.Fatalf("recipe create = %+v, calls %d, err %v", record, calls, err)
	}
	if clones := vm.CloneCalls(); len(clones) != 1 || clones[0].SourceID != prepared {
		t.Fatalf("recipe create cloned wrong source: %+v", clones)
	}
	current, err := golden.LoadCurrent(t.Context(), selected)
	if err != nil || current.Revision != legacy {
		t.Fatalf("recipe create changed current golden: %+v, %v", current, err)
	}
	if output.String() != "domain: alpha\nsession: dev\nmode: clean\nstate: stopped\n" {
		t.Fatalf("recipe create output: %q", output.String())
	}
}

func TestRecipeSessionCreateRejectsIncompleteOrInvalidReceiptBeforeClone(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	vm := fake.New(backend.Observation{ObjectID: "golden-alpha-legacy", Exists: true, State: backend.ObjectStopped})
	if _, err := golden.Register(t.Context(), selected, "golden-alpha-legacy", vm); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	base := []string{"--config", configPath, "--domain", "alpha", "session", "create"}
	for _, suffix := range [][]string{
		{"--recipe", "", "dev"},
		{"--recipe", filepath.Join(root, "recipe.json"), "dev"},
		{"--iso", filepath.Join(root, "ubuntu.iso"), "dev"},
	} {
		called := false
		err := Run(t.Context(), append(append([]string(nil), base...), suffix...), Options{Observer: vm, Creator: vm, Output: &bytes.Buffer{}, AlphaPrepare: func(context.Context, config.Config, config.Domain, string, AlphaPrepareInput) (basebuild.PreparedResult, error) {
			called = true
			return basebuild.PreparedResult{}, nil
		}})
		if err == nil || called || len(vm.CloneCalls()) != 0 {
			t.Fatalf("incomplete recipe flags reached preparation or clone: %q, %v", suffix, err)
		}
	}
	input := AlphaPrepareInput{RecipePath: filepath.Join(root, "recipe.json"), ISOPath: filepath.Join(root, "ubuntu.iso"), GuestDefinitionRoot: filepath.Join(root, "guest"), OpenSSLPath: "/usr/bin/openssl", OpenSSLSHA256: strings.Repeat("a", 64), XorrisoPath: "/usr/bin/xorriso", XorrisoSHA256: strings.Repeat("b", 64)}
	args := append(append([]string(nil), base...), "--recipe", input.RecipePath, "--iso", input.ISOPath, "--guest-definition", input.GuestDefinitionRoot, "--openssl", input.OpenSSLPath, "--openssl-sha256", input.OpenSSLSHA256, "--xorriso", input.XorrisoPath, "--xorriso-sha256", input.XorrisoSHA256, "dev")
	err := Run(t.Context(), args, Options{Observer: vm, Creator: vm, Output: &bytes.Buffer{}, AlphaPrepare: func(context.Context, config.Config, config.Domain, string, AlphaPrepareInput) (basebuild.PreparedResult, error) {
		return basebuild.PreparedResult{Disposition: basebuild.PreparedBuilt, Record: basebuild.PreparedRecord{Version: 2, CandidateID: "golden-alpha-legacy"}}, nil
	}})
	if err == nil || len(vm.CloneCalls()) != 0 {
		t.Fatalf("invalid preparation receipt reached clone: %v", err)
	}
}

func TestBackendFactoryIsUnreachableForInvalidInputAndOtherCommands(t *testing.T) {
	path, _ := writeV2DomainFixture(t, "work")
	for _, args := range [][]string{
		{"--config", path + ".missing", "--domain", "work", "golden", "register", "golden-work-r1"},
		{"--config", path, "--domain", "unknown", "session", "create", "dev"},
		{"--config", path, "session", "status", "dev"},
		{"--config", path, "--domain", "work", "session", "status", "../bad"},
		{"--config", path, "--domain", "work", "session", "create", "../bad"},
		{"--config", path, "--domain", "work", "golden", "register", "../bad"},
		{"--config", path, "--domain", "work", "session", "create", "--mode", "bad", "dev"},
		{"--config", path, "--domain", "work", "session", "status", "dev", "extra"},
		{"--config", path, "--domain", "work", "session", "start", "dev"},
		{"--config", path, "--domain", "work", "domain", "init"},
		{"--config", path, "init"},
		{"--config", path, "doctor"},
	} {
		t.Run(strings.Join(args[2:], "_"), func(t *testing.T) {
			err := Run(context.Background(), args, Options{Output: &bytes.Buffer{}, BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
				t.Fatal("unadmitted/non-backend command reached factory")
				return BackendDependencies{}, nil
			}})
			if err == nil {
				t.Fatal("invalid or unavailable command succeeded")
			}
		})
	}
}

func TestBackendFactoryFailureDoesNotUseReturnedOrDirectDependencies(t *testing.T) {
	for _, suffix := range [][]string{{"golden", "register", "golden-work-r1"}, {"session", "create", "dev"}, {"session", "status", "dev"}} {
		t.Run(strings.Join(suffix, "_"), func(t *testing.T) {
			path, selected := writeV2DomainFixture(t, "work")
			before, err := os.ReadDir(selected.StateRoot)
			if err != nil {
				t.Fatal(err)
			}
			observer, creator := &countingObserver{}, fake.New()
			want := errors.New("backend construction denied")
			var output bytes.Buffer
			err = Run(context.Background(), append([]string{"--config", path, "--domain", "work"}, suffix...), Options{
				Output: &output, Observer: observer, Creator: creator,
				BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
					return BackendDependencies{Observer: observer, Creator: creator}, want
				},
			})
			if !errors.Is(err, want) || output.Len() != 0 {
				t.Fatalf("factory failure = %v, output %q", err, output.String())
			}
			if observer.calls != 0 || len(creator.CloneCalls()) != 0 || len(creator.RandomizeMACCalls()) != 0 {
				t.Fatal("factory failure accessed backend")
			}
			after, err := os.ReadDir(selected.StateRoot)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("factory failure changed state directory: %v, %v", after, err)
			}
		})
	}
}

func TestBackendFactoryMissingDependenciesNeverFallBackToDirectInjection(t *testing.T) {
	path, _ := writeV2DomainFixture(t, "work")
	observer, creator := &countingObserver{}, fake.New()
	for _, deps := range []BackendDependencies{{}, {Observer: observer}, {Creator: creator}} {
		err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "create", "dev"}, Options{
			Output: &bytes.Buffer{}, Observer: observer, Creator: creator,
			BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) { return deps, nil },
		})
		if err == nil || (!strings.Contains(err.Error(), "observer is required") && !strings.Contains(err.Error(), "creator is required")) {
			t.Fatalf("missing dependency refusal = %v", err)
		}
		if observer.calls != 0 || len(creator.CloneCalls()) != 0 || len(creator.RandomizeMACCalls()) != 0 {
			t.Fatal("missing factory dependency accessed backend")
		}
	}
}

// Production break: omitting the public start dispatch would leave the
// generation-safe session service unreachable from the supported CLI surface.
func TestSessionStartDispatchesOnlySelectedDomainStarter(t *testing.T) {
	configPath, _ := writeDomainFixture(t, "work")
	starter := &sessionStarterFake{record: session.Record{Domain: "work", Name: "dev", IntendedState: session.StateRunning, Readiness: session.ReadinessRecord{Status: session.ReadinessReady}}}
	var output bytes.Buffer
	if err := Run(context.Background(), []string{"--config", configPath, "--domain", "work", "session", "start", "dev"}, Options{SessionStarter: starter, Output: &output}); err != nil {
		t.Fatalf("Run(session start) error = %v", err)
	}
	if starter.name != "dev" {
		t.Fatalf("Start() name = %q, want dev", starter.name)
	}
	if got, want := output.String(), "domain: work\nsession: dev\nstate: running\nreadiness: ready\n"; got != want {
		t.Fatalf("Run(session start) output = %q, want %q", got, want)
	}
}

func TestSessionStarterFactoryReceivesAdmittedConfigDomainAndExactPath(t *testing.T) {
	path := writeV2DomainSetFixture(t)
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	selected, _ := loaded.Domain("work")
	starter := &sessionStarterFake{record: session.Record{Domain: "work", Name: "dev", IntendedState: session.StateStarting, Readiness: session.ReadinessRecord{Status: session.ReadinessStarting}}}
	var output bytes.Buffer
	calls := 0
	err = Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "start", "dev"}, Options{
		ConfigPath: "/unused/config.json", Output: &output,
		SessionStarterFactory: func(got config.Config, domain config.Domain, exactPath string) (SessionStarter, error) {
			calls++
			if !reflect.DeepEqual(got, loaded) || domain != selected || exactPath != path {
				t.Fatalf("factory inputs = %#v %#v %q", got, domain, exactPath)
			}
			return starter, nil
		},
	})
	if err != nil || calls != 1 || starter.name != "dev" {
		t.Fatalf("Run = %v, factory calls %d, name %q", err, calls, starter.name)
	}
	if got, want := output.String(), "domain: work\nsession: dev\nstate: starting\nreadiness: starting\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestSessionStarterFactoryIsUnreachableBeforeAdmissionAndForOtherCommands(t *testing.T) {
	path, _ := writeV2DomainFixture(t, "work")
	for _, args := range [][]string{
		{"--config", path + ".missing", "--domain", "work", "session", "start", "dev"},
		{"--config", path, "--domain", "unknown", "session", "start", "dev"},
		{"--config", path, "session", "start", "dev"},
		{"--config", path, "--domain", "work", "session", "start", "../bad"},
		{"--config", path, "--domain", "work", "session", "start", "dev", "extra"},
		{"--config", path, "--domain", "work", "session", "stop", "dev"},
		{"--config", path, "--domain", "work", "session", "status", "dev"},
	} {
		t.Run(strings.Join(args[2:], "_"), func(t *testing.T) {
			err := Run(context.Background(), args, Options{Output: &bytes.Buffer{}, SessionStarterFactory: func(config.Config, config.Domain, string) (SessionStarter, error) {
				t.Fatal("unadmitted/non-start command reached factory")
				return nil, nil
			}})
			if err == nil {
				t.Fatal("invalid or unavailable command succeeded")
			}
		})
	}
}

func TestSessionStarterFactoryFailureAndNilStarterAreErrors(t *testing.T) {
	path, _ := writeDomainFixture(t, "work")
	want := errors.New("construction denied")
	for _, factoryErr := range []error{want, nil} {
		err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "start", "dev"}, Options{Output: &bytes.Buffer{}, SessionStarterFactory: func(config.Config, config.Domain, string) (SessionStarter, error) { return nil, factoryErr }})
		if err == nil || (factoryErr != nil && !errors.Is(err, want)) {
			t.Fatalf("factory failure = %v", err)
		}
	}
}

type sessionStarterFake struct {
	name   string
	record session.Record
}

func (s *sessionStarterFake) Start(_ context.Context, name string) (session.Record, error) {
	s.name = name
	return s.record, nil
}

type sessionStopperFake struct {
	name   string
	record session.Record
}

func (s *sessionStopperFake) Stop(_ context.Context, name string) (session.Record, error) {
	s.name = name
	return s.record, nil
}

func TestSessionStopUsesExactAdmittedFactoryAndReportsStoppedState(t *testing.T) {
	path, selected := writeV2DomainFixture(t, "work")
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stopper := &sessionStopperFake{record: session.Record{Domain: selected.ID, Name: "dev", IntendedState: session.StateStopped, Readiness: session.ReadinessRecord{Status: session.ReadinessNotReady}}}
	var output bytes.Buffer
	calls := 0
	err = Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "stop", "dev"}, Options{
		Output: &output,
		SessionStopperFactory: func(got config.Config, domain config.Domain, exactPath string) (SessionStopper, error) {
			calls++
			if !reflect.DeepEqual(got, loaded) || domain != selected || exactPath != path {
				t.Fatalf("stop factory inputs = %#v, %#v, %q", got, domain, exactPath)
			}
			return stopper, nil
		},
	})
	if err != nil || calls != 1 || stopper.name != "dev" || output.String() != "domain: work\nsession: dev\nstate: stopped\nreadiness: not_ready\n" {
		t.Fatalf("stop = %v; calls=%d name=%q output=%q", err, calls, stopper.name, output.String())
	}
}

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
		StatusSnapshotFactory: func(config.Config, config.Domain) (StatusSnapshotReader, error) {
			t.Fatal("stopped status must not ask for supervisor evidence")
			return nil, nil
		},
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

type statusSnapshotFake struct {
	snapshot supervisor.Snapshot
	err      error
	calls    int
	binding  supervisor.Binding
}

func (f *statusSnapshotFake) Snapshot(_ context.Context, binding supervisor.Binding) (supervisor.Snapshot, error) {
	f.calls++
	f.binding = binding
	return f.snapshot, f.err
}

func writeRunningStatusFixture(t *testing.T, readiness session.ReadinessStatus) (string, []byte) {
	t.Helper()
	path := writeStatusFixture(t, "work", "dev")
	recordPath := filepath.Join(filepath.Dir(path), "sessions", "dev.json")
	record := []byte(fmt.Sprintf(`{"version":2,"domain":"work","name":"dev","id":"00000000-0000-4000-8000-000000000001","mode":"clean","intended_state":"running","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-work-r1","start_generation":"11111111-2222-4333-8444-555555555555","readiness":{"status":%q,"diagnostic":""}}`, readiness))
	if err := os.WriteFile(recordPath, record, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, record
}

func TestSessionStatusRequiresFreshExactReadyEvidence(t *testing.T) {
	now := time.Now().UTC()
	binding := supervisor.Binding{Domain: "work", SessionID: "00000000-0000-4000-8000-000000000001", BackendKind: "tart", BackendObject: "boxwarden-work-dev", Generation: "11111111-2222-4333-8444-555555555555"}
	ready := supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: now}
	for _, test := range []struct {
		name      string
		mutate    func(*supervisor.Snapshot)
		readerErr error
		want      string
	}{
		{name: "ready", want: "ready"},
		{name: "wrong generation", mutate: func(s *supervisor.Snapshot) { s.Binding.Generation = "foreign" }, want: "drift"},
		{name: "stale", mutate: func(s *supervisor.Snapshot) { s.ObservedAt = now.Add(-time.Minute) }, want: "drift"},
		{name: "future", mutate: func(s *supervisor.Snapshot) { s.ObservedAt = now.Add(time.Minute) }, want: "drift"},
		{name: "backend not proved", mutate: func(s *supervisor.Snapshot) { s.BackendRunning = false }, want: "drift"},
		{name: "serial poisoned", mutate: func(s *supervisor.Snapshot) { s.SerialHealthy = false }, want: "drift"},
		{name: "pin absent", mutate: func(s *supervisor.Snapshot) { s.PinPresent = false }, want: "drift"},
		{name: "certificate stale", mutate: func(s *supervisor.Snapshot) { s.CertificateCurrent = false }, want: "drift"},
		{name: "probe failed", mutate: func(s *supervisor.Snapshot) { s.ProbeOK = false }, want: "drift"},
		{name: "zone mismatch", mutate: func(s *supervisor.Snapshot) { s.ZoneMatches = false }, want: "drift"},
		{name: "supervisor unavailable", readerErr: errors.New("control socket unavailable"), want: "drift"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, before := writeRunningStatusFixture(t, session.ReadinessReady)
			snapshot := ready
			if test.mutate != nil {
				test.mutate(&snapshot)
			}
			reader := &statusSnapshotFake{snapshot: snapshot, err: test.readerErr}
			var output bytes.Buffer
			err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "status", "dev"}, Options{
				Observer: fake.Observer{Observations: map[string]backend.Observation{"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning}}},
				StatusSnapshotFactory: func(loaded config.Config, selected config.Domain) (StatusSnapshotReader, error) {
					if selected.ID != "work" || selected.StateRoot != filepath.Dir(path) {
						t.Fatalf("status factory got wrong domain: %#v", selected)
					}
					return reader, nil
				},
				Output: &output,
			})
			if err != nil || reader.calls != 1 || reader.binding != binding {
				t.Fatalf("status error/calls/binding = %v/%d/%#v", err, reader.calls, reader.binding)
			}
			if !strings.Contains(output.String(), "readiness: "+test.want+"\n") || !strings.Contains(output.String(), "consistency: "+map[string]string{"ready": "consistent", "drift": "drift"}[test.want]+"\n") {
				t.Fatalf("status output = %q, want live readiness %q", output.String(), test.want)
			}
			after, err := os.ReadFile(filepath.Join(filepath.Dir(path), "sessions", "dev.json"))
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("status changed durable record: %v", err)
			}
		})
	}
}

func TestSessionStatusKeepsContradictoryTartListingVisibleWithExactReadyOwner(t *testing.T) {
	path, before := writeRunningStatusFixture(t, session.ReadinessReady)
	binding := supervisor.Binding{Domain: "work", SessionID: "00000000-0000-4000-8000-000000000001", BackendKind: "tart", BackendObject: "boxwarden-work-dev", Generation: "11111111-2222-4333-8444-555555555555"}
	reader := &statusSnapshotFake{snapshot: supervisor.Snapshot{Binding: binding, BackendRunning: true, SerialHealthy: true, PinPresent: true, CertificateCurrent: true, ProbeOK: true, ZoneMatches: true, ObservedAt: time.Now()}}
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "status", "dev"}, Options{
		Observer:              fake.Observer{Observations: map[string]backend.Observation{"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectStopped}}},
		StatusSnapshotFactory: func(config.Config, config.Domain) (StatusSnapshotReader, error) { return reader, nil },
		Output:                &output,
	})
	if err != nil || reader.calls != 1 || reader.binding != binding || !strings.Contains(output.String(), "observed: stopped\n") || !strings.Contains(output.String(), "consistency: consistent\nreadiness: ready\n") || !strings.Contains(output.String(), "Tart listing reports stopped") {
		t.Fatalf("contradictory status = %q; error=%v; reader=%+v", output.String(), err, reader)
	}
	after, err := os.ReadFile(filepath.Join(filepath.Dir(path), "sessions", "dev.json"))
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("status mutated durable state: %v", err)
	}
}

func TestSessionStatusReportsDriftWithoutSupervisorReader(t *testing.T) {
	path, _ := writeRunningStatusFixture(t, session.ReadinessReady)
	var output bytes.Buffer
	err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "status", "dev"}, Options{
		Observer: fake.Observer{Observations: map[string]backend.Observation{"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: backend.ObjectRunning}}},
		Output:   &output,
	})
	if err != nil || !strings.Contains(output.String(), "consistency: drift\nreadiness: drift\n") || !strings.Contains(output.String(), "exact live supervisor readiness is unavailable") {
		t.Fatalf("missing supervisor status = %q, %v", output.String(), err)
	}
}

func TestSessionStatusDoesNotPromotePersistedDriftOrStoppedBackend(t *testing.T) {
	for _, test := range []struct {
		name      string
		readiness session.ReadinessStatus
		backend   backend.ObjectState
	}{
		{name: "persisted drift", readiness: session.ReadinessDrift, backend: backend.ObjectRunning},
		{name: "backend stopped", readiness: session.ReadinessReady, backend: backend.ObjectStopped},
	} {
		t.Run(test.name, func(t *testing.T) {
			path, _ := writeRunningStatusFixture(t, test.readiness)
			var output bytes.Buffer
			readerCalls := 0
			err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "status", "dev"}, Options{
				Observer: fake.Observer{Observations: map[string]backend.Observation{"boxwarden-work-dev": {ObjectID: "boxwarden-work-dev", Exists: true, State: test.backend}}},
				StatusSnapshotFactory: func(config.Config, config.Domain) (StatusSnapshotReader, error) {
					readerCalls++
					return &statusSnapshotFake{}, nil
				},
				Output: &output,
			})
			wantCalls := 0
			if test.backend == backend.ObjectStopped && test.readiness == session.ReadinessReady {
				wantCalls = 1
			}
			if err != nil || !strings.Contains(output.String(), "readiness: drift\n") || !strings.Contains(output.String(), "consistency: drift\n") || readerCalls != wantCalls {
				t.Fatalf("status = %q, err=%v reader calls=%d", output.String(), err, readerCalls)
			}
		})
	}
}

func TestSessionStatusRejectsForeignBackendObservationBeforeSupervisor(t *testing.T) {
	path, _ := writeRunningStatusFixture(t, session.ReadinessReady)
	var output bytes.Buffer
	readerCalls := 0
	err := Run(context.Background(), []string{"--config", path, "--domain", "work", "session", "status", "dev"}, Options{
		Observer: fake.Observer{Observations: map[string]backend.Observation{"boxwarden-work-dev": {ObjectID: "boxwarden-work-foreign", Exists: true, State: backend.ObjectRunning}}},
		StatusSnapshotFactory: func(config.Config, config.Domain) (StatusSnapshotReader, error) {
			readerCalls++
			return &statusSnapshotFake{}, nil
		},
		Output: &output,
	})
	if err != nil || readerCalls != 0 || !strings.Contains(output.String(), "consistency: drift\n") || !strings.Contains(output.String(), "readiness: drift\n") {
		t.Fatalf("foreign backend status = %q, err=%v reader calls=%d", output.String(), err, readerCalls)
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
				err := Run(context.Background(), args, Options{Observer: observer, Output: &bytes.Buffer{}, BackendFactory: func(config.Config, config.Domain) (BackendDependencies, error) {
					t.Fatal("invalid host configuration reached backend factory")
					return BackendDependencies{}, nil
				}})
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
