package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/workspacex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProjectSupportedRecipeSelection(t *testing.T) {
	for _, op := range []string{"create", "rebuild"} {
		for _, name := range []string{"desktop", "actions", "chatgpt"} {
			if _, err := parseProject([]string{op, "--recipe", name, "demo"}); err != nil {
				t.Fatalf("%s recipe %s: %v", op, name, err)
			}
		}
		for _, args := range [][]string{
			{op, "--recipe", "unknown", "demo"},
			{op, "--recipe", "chatgpt", "--base", "current", "demo"},
			{op, "--recipe", "../chatgpt", "demo"},
		} {
			if _, err := parseProject(args); err == nil {
				t.Fatalf("accepted %v", args)
			}
		}
	}
	if _, err := parseProject([]string{"rebuild", "retry", "--recipe", "chatgpt", "demo"}); err == nil {
		t.Fatal("retry changed frozen recipe")
	}
}

func TestProjectRecipeSetupRequiresCompletePinnedTools(t *testing.T) {
	base := []string{"setup", "--source-root", "/source", "--formatter-bundle", "/formatter", "--iso", "/ubuntu.iso", "--go", "/tool/go"}
	tools := []string{"--openssl", "/tool/openssl", "--openssl-sha256", strings.Repeat("a", 64), "--xorriso", "/tool/xorriso", "--xorriso-sha256", strings.Repeat("b", 64)}
	if _, err := parseProject(append(append([]string{}, base...), tools...)); err != nil {
		t.Fatal(err)
	}
	if _, err := parseProject(append(append([]string{}, base...), tools[:2]...)); err == nil {
		t.Fatal("accepted partial recipe prerequisites")
	}
	if _, err := parseProject(append(append([]string{}, base...), tools[:6]...)); err == nil {
		t.Fatal("accepted missing tool hash")
	}
}

func recipeEnabledFixture(t *testing.T) ([]string, config.Domain, Options, *fake.Backend, *bytes.Buffer) {
	t.Helper()
	prefix, d, o, b, out := projectFixture(t)
	args := []string{"project", "setup-update", "--source-root", "/source", "--formatter-bundle", "/formatter", "--iso", "/ubuntu.iso", "--go", "/tool/go", "--openssl", "/tool/openssl", "--openssl-sha256", strings.Repeat("a", 64), "--xorriso", "/tool/xorriso", "--xorriso-sha256", strings.Repeat("b", 64)}
	if err := Run(t.Context(), append(prefix, args...), o); err != nil {
		t.Fatal(err)
	}
	return prefix, d, o, b, out
}

type unavailableRecipeStarter struct{}

func (unavailableRecipeStarter) Start(context.Context, string) (session.Record, error) {
	return session.Record{}, errors.New("synthetic startup unavailable")
}

func preparedRecipeFixture(t *testing.T, d config.Domain) AlphaPrepared {
	t.Helper()
	value, err := recipe.LoadRunnable(filepath.Join("..", "..", "examples", "v0.2-alpha-chatgpt.json"))
	if err != nil {
		t.Fatal(err)
	}
	digest, err := session.PublishRecipeIntent(d.StateRoot, value)
	if err != nil {
		t.Fatal(err)
	}
	key := strings.Repeat("a", 64)
	return AlphaPrepared{IntentDigest: digest, Base: basebuild.PreparedResult{Disposition: basebuild.PreparedReused, Record: basebuild.PreparedRecord{Version: 2, CandidateID: "golden-desktop", PreparationKey: key, CandidateIdentity: strings.Repeat("b", 64), AttemptDirectory: filepath.Join(d.StateRoot, "prepared-attempts", "synthetic-preparation"), Qualification: basebuild.QualificationReceipt{CandidateID: "golden-desktop", CloneID: "synthetic-qualification", PreparationKey: key, EvidenceSHA256: strings.Repeat("c", 64), BOMSHA256: strings.Repeat("d", 64), Passed: true}}}}
}

func TestProjectRecipeCreationRetainsBindingAndOpenNeverPreparesAgain(t *testing.T) {
	prefix, d, o, b, _ := recipeEnabledFixture(t)
	prepared := preparedRecipeFixture(t, d)
	preparations := 0
	o.AlphaPrepare = func(_ context.Context, _ config.Config, _ config.Domain, _ string, in AlphaPrepareInput) (AlphaPrepared, error) {
		preparations++
		if !in.RequireGuestSupport || in.CapturedIntentDigest != "" || filepath.Base(in.RecipePath) != "v0.2-alpha-chatgpt.json" || in.OpenSSLPath != "/tool/openssl" {
			t.Fatalf("wrong preparation intent: %+v", in)
		}
		return prepared, nil
	}
	o.SessionStarter = unavailableRecipeStarter{}
	for _, name := range []string{"first", "second"} {
		err := Run(t.Context(), append(prefix, "project", "create", "--recipe", "chatgpt", name), o)
		if err == nil || !strings.Contains(err.Error(), "synthetic startup unavailable") {
			t.Fatal(err)
		}
		r, err := projectx.Load(d.StateRoot, d.ID, name)
		if err != nil {
			t.Fatal(err)
		}
		s, err := session.LoadRecord(d.StateRoot, string(d.ID), name)
		if err != nil {
			t.Fatal(err)
		}
		if r.Version != 2 || r.RecipeIntentDigest != prepared.IntentDigest || s.RecipeIntentDigest != r.RecipeIntentDigest || !r.Initialized {
			t.Fatal("lost retained immutable recipe/workspace binding")
		}
	}
	if preparations != 2 || len(b.CloneCalls()) != 2 {
		t.Fatal("wrong new-project allocation count")
	}
	if err := Run(t.Context(), append(prefix, "project", "open", "first"), o); err == nil {
		t.Fatal("unexpected synthetic start success")
	}
	if preparations != 2 || len(b.CloneCalls()) != 2 {
		t.Fatal("ordinary open prepared or cloned")
	}
	o.AlphaPrepare = func(context.Context, config.Config, config.Domain, string, AlphaPrepareInput) (AlphaPrepared, error) {
		return AlphaPrepared{}, errors.New("synthetic preparation failed")
	}
	if err := Run(t.Context(), append(prefix, "project", "create", "--recipe", "chatgpt", "failed"), o); err == nil {
		t.Fatal("failed preparation accepted")
	}
	if _, err := projectx.Load(d.StateRoot, d.ID, "failed"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed preparation reserved project")
	}
	if len(b.CloneCalls()) != 2 {
		t.Fatal("failed preparation cloned")
	}
}

func TestProjectRecipeReplacementFreezesIntentAndRetainsEditedWorkspaceBinding(t *testing.T) {
	prefix, d, o, b, _ := recipeEnabledFixture(t)
	if err := Run(t.Context(), append(prefix, "project", "create", "--base", "current", "demo"), o); err != nil {
		t.Fatal(err)
	}
	before, _ := projectx.Load(d.StateRoot, d.ID, "demo")
	before.ImportID = "00112233-4455-6677-8899-aabbccddeeff"
	before.ImportSource = "/private/source"
	before.Imported = true
	if err := projectx.Save(d.StateRoot, d.ID, before); err != nil {
		t.Fatal(err)
	}
	vBefore, err := workspacex.LoadRecord(d.StateRoot, d.ID, before.VolumeID)
	if err != nil {
		t.Fatal(err)
	}
	prepared := preparedRecipeFixture(t, d)
	preparations := 0
	o.AlphaPrepare = func(context.Context, config.Config, config.Domain, string, AlphaPrepareInput) (AlphaPrepared, error) {
		preparations++
		return prepared, nil
	}
	o.AlphaRebuildPrepare = nil
	o.AlphaRebuildPrepareWithIntent = func(_ context.Context, _ config.Config, _ config.Domain, _ string, name, base, digest string) (session.RebuildJournal, error) {
		if name != before.Name || base != "golden-desktop" || digest != prepared.IntentDigest {
			t.Fatal("wrong new-system recipe tuple")
		}
		return session.RebuildJournal{Version: 1, Domain: d.ID, SessionName: name, SessionID: before.SessionID, OperationID: "77772233-4455-6677-8899-aabbccddeeff", Phase: session.RebuildCloned, OldBackend: before.BackendObject, OldRevision: before.Base, CandidateBackend: "boxwarden-alpha-77772233445566778899aabbccddeeff", CandidateRevision: base, CandidateIntentDigest: digest}, nil
	}
	o.AlphaRebuildCandidate = func(_ context.Context, _ config.Config, _ config.Domain, _ string, j session.RebuildJournal) (session.Record, error) {
		if j.CandidateIntentDigest != prepared.IntentDigest {
			t.Fatal("recipe lost in frozen witness")
		}
		s, err := session.LoadRecord(d.StateRoot, string(d.ID), before.Name)
		if err != nil {
			t.Fatal(err)
		}
		s.Backend.ObjectID = j.CandidateBackend
		s.GoldenRevision = j.CandidateRevision
		s.RecipeIntentDigest = j.CandidateIntentDigest
		if err := session.SaveRecord(d.StateRoot, d.ID, s); err != nil {
			t.Fatal(err)
		}
		b.SetObservation(backend.Observation{ObjectID: j.CandidateBackend, Exists: true, State: backend.ObjectStopped})
		return s, nil
	}
	o.SessionStarter = unavailableRecipeStarter{}
	err = Run(t.Context(), append(prefix, "project", "rebuild", "--recipe", "chatgpt", "demo"), o)
	if err == nil || !strings.Contains(err.Error(), "software setup") {
		t.Fatalf("missing post-cutover setup outcome: %v", err)
	}
	after, err := projectx.Load(d.StateRoot, d.ID, "demo")
	if err != nil {
		t.Fatal(err)
	}
	want := before
	want.Version = 2
	want.BackendObject = "boxwarden-alpha-77772233445566778899aabbccddeeff"
	want.RecipeIntentDigest = prepared.IntentDigest
	if after != want {
		t.Fatalf("replacement lost preserved work/import or recipe: %+v", after)
	}
	vAfter, err := workspacex.LoadRecord(d.StateRoot, d.ID, before.VolumeID)
	if err != nil || !reflect.DeepEqual(vBefore, vAfter) {
		t.Fatal("workspace rewritten")
	}
	if preparations != 1 {
		t.Fatal("replacement duplicated preparation")
	}
	if err := Run(t.Context(), append(prefix, "project", "open", "demo"), o); err == nil {
		t.Fatal("unexpected synthetic open success")
	}
	if preparations != 1 {
		t.Fatal("open re-prepared replacement")
	}
}
