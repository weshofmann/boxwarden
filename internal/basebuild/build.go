// Package basebuild coordinates one disposable generic-base candidate build.
// It intentionally cannot qualify or admit the candidate to a reusable cache.
package basebuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

const (
	CloneReadyMarker = "generic golden clone-ready; power off without another boot"
	FinalizerCommand = "sudo -n -- /usr/local/libexec/boxwarden-finalize-golden --acknowledge-generic-golden-finalization"
	PoweroffCommand  = "sudo -n -- /usr/bin/systemctl poweroff"
)

func InstalledPrompt(runID string) string { return "boxwarden@boxwarden-task0-" + runID + ":" }

type Phase string

const (
	PhaseReserved         Phase = "reserved"
	PhaseRendered         Phase = "rendered"
	PhaseCreated          Phase = "created"
	PhaseRunning          Phase = "installer-running"
	PhaseFinalizing       Phase = "finalizing"
	PhaseStopping         Phase = "stopping"
	PhaseCandidateStopped Phase = "candidate-stopped"
	PhaseQualifying       Phase = "qualifying"
	PhaseAdmissionPending Phase = "admission-pending"
	PhaseAdmitted         Phase = "admitted"
	PhaseFailed           Phase = "failed"
)

type Inputs struct {
	AttemptRoot         string
	AttemptID           string
	CandidateID         string
	RunID               string
	ISOPath             string
	GuestDefinitionRoot string
	Recipe              recipe.Recipe
}

type Attempt struct {
	Version           int                   `json:"version"`
	CandidateID       string                `json:"candidate_id"`
	PreparationKey    string                `json:"preparation_key"`
	Phase             Phase                 `json:"phase"`
	Failure           string                `json:"failure,omitempty"`
	Qualification     *QualificationReceipt `json:"qualification,omitempty"`
	CandidateIdentity string                `json:"candidate_identity,omitempty"`
}

type Result struct {
	AttemptDirectory string
	CandidateID      string
	PreparationKey   string
	State            CandidateState
	Qualified        bool
	CacheAdmitted    bool
}

type CandidateState string

const CandidateStopped CandidateState = "stopped-unqualified"

// Checks verifies source intent and stages the exact bytes used by the build.
type Checks interface {
	Verify(Inputs) (string, error)
	Stage(context.Context, Inputs, string, string) (Inputs, error)
}

type RecipeChecks struct{}

func (RecipeChecks) Verify(in Inputs) (string, error) {
	if err := recipe.VerifyISO(in.ISOPath); err != nil {
		return "", err
	}
	digest, err := recipe.GuestDefinitionDigest(in.GuestDefinitionRoot)
	if err != nil {
		return "", err
	}
	return recipe.PreparationKey(in.Recipe, digest)
}

func (c RecipeChecks) Stage(ctx context.Context, in Inputs, attemptDir, expectedKey string) (Inputs, error) {
	staged, err := stageBuildInputs(ctx, in, attemptDir)
	if err != nil {
		return Inputs{}, err
	}
	key, err := c.Verify(staged)
	if err != nil {
		return Inputs{}, fmt.Errorf("verify staged build inputs: %w", err)
	}
	if key != expectedKey {
		return Inputs{}, errors.New("staged build inputs differ from verified preparation key")
	}
	return staged, nil
}

// SeedBuilder is deliberately separate from the guest recipe. Its verifier
// provider must create a fresh private SHA-512 crypt file for the builder only;
// no production provider is supplied by this foundation package.
type SeedBuilder interface {
	BuilderVerifier(context.Context, string) (string, error)
	Render(context.Context, RenderRequest) error
	Remaster(context.Context, RemasterRequest) error
}

type RenderRequest struct {
	GuestDefinitionRoot string
	RunID               string
	VerifierFile        string
	OutputDirectory     string
}
type RemasterRequest struct {
	GuestDefinitionRoot string
	SourceISO           string
	RenderedUserData    string
	OutputISO           string
}
type CreateRequest struct {
	CandidateID string
	DiskGiB     int
}
type ConfigureRequest struct {
	CandidateID string
	CPUs        int
	MemoryMiB   int
}
type InstallerRequest struct {
	CandidateID     string
	RunID           string
	ISOPath         string
	SerialDirectory string
}

type VirtualMachine interface {
	backend.Observer
	Create(context.Context, CreateRequest) error
	Configure(context.Context, ConfigureRequest) error
	RunInstaller(context.Context, InstallerRequest) (InstallerHandle, error)
}

// InstallerHandle must own and reap the exact Tart child. WaitFor must use a
// bounded serial reader and return only for the exact marker supplied here.
type InstallerHandle interface {
	WaitFor(context.Context, string) error
	SendLine(context.Context, string) error
	Stop(context.Context) error
	Wait(context.Context) error
}

type Dependencies struct {
	Checks Checks
	Seed   SeedBuilder
	VM     VirtualMachine
}

// Build reserves exactly one fresh attempt and leaves its candidate stopped.
// A failed attempt is retained for diagnosis and is never resumed implicitly.
func Build(ctx context.Context, in Inputs, deps Dependencies) (result Result, err error) {
	if err := validate(in, deps); err != nil {
		return Result{}, err
	}
	key, err := deps.Checks.Verify(in)
	if err != nil {
		return Result{}, fmt.Errorf("verify build inputs: %w", err)
	}
	if len(key) != 64 {
		return Result{}, errors.New("verification returned an invalid preparation key")
	}
	prior, err := deps.VM.Observe(ctx, in.CandidateID)
	if err != nil {
		return Result{}, fmt.Errorf("observe candidate before build: %w", err)
	}
	if prior.ObjectID != in.CandidateID || prior.Exists {
		return Result{}, errors.New("candidate object is occupied or observation mismatched")
	}

	attemptDir := filepath.Join(in.AttemptRoot, in.AttemptID)
	if err := os.Mkdir(attemptDir, 0700); err != nil {
		return Result{}, fmt.Errorf("reserve private build attempt: %w", err)
	}
	state := Attempt{Version: 1, CandidateID: in.CandidateID, PreparationKey: key, Phase: PhaseReserved}
	if err := writeAttempt(attemptDir, state); err != nil {
		return Result{}, err
	}
	defer func() {
		if err != nil {
			failedDuring := state.Phase
			state.Phase = PhaseFailed
			state.Failure = "build failed during " + string(failedDuring)
			err = errors.Join(err, writeAttempt(attemptDir, state))
		}
	}()
	setPhase := func(phase Phase) error { state.Phase = phase; return writeAttempt(attemptDir, state) }
	staged, err := deps.Checks.Stage(ctx, in, attemptDir, key)
	if err != nil {
		return Result{}, fmt.Errorf("stage exact build inputs: %w", err)
	}
	if staged.ISOPath == "" || staged.GuestDefinitionRoot == "" {
		return Result{}, errors.New("staged build inputs are incomplete")
	}

	verifier, err := deps.Seed.BuilderVerifier(ctx, attemptDir)
	if privateVerifierPath(attemptDir, verifier) {
		defer func() {
			if errors.Is(err, ErrScriptReapUnproven) {
				return // An unproven script may still be reading this staging input.
			}
			if removeErr := os.Remove(verifier); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				result = Result{}
				err = errors.Join(err, fmt.Errorf("remove temporary builder verifier: %w", removeErr))
			}
		}()
	}
	if err != nil {
		return Result{}, fmt.Errorf("create private builder verifier: %w", err)
	}
	if err := validateVerifier(attemptDir, verifier); err != nil {
		return Result{}, err
	}
	seedDir := filepath.Join(attemptDir, "seed")
	if err := deps.Seed.Render(ctx, RenderRequest{GuestDefinitionRoot: staged.GuestDefinitionRoot, RunID: in.RunID, VerifierFile: verifier, OutputDirectory: seedDir}); err != nil {
		return Result{}, fmt.Errorf("render installer seed: %w", err)
	}
	installerISO := filepath.Join(attemptDir, "installer.iso")
	if err := deps.Seed.Remaster(ctx, RemasterRequest{GuestDefinitionRoot: staged.GuestDefinitionRoot, SourceISO: staged.ISOPath, RenderedUserData: filepath.Join(seedDir, "user-data"), OutputISO: installerISO}); err != nil {
		return Result{}, fmt.Errorf("remaster installer: %w", err)
	}
	if err := setPhase(PhaseRendered); err != nil {
		return Result{}, err
	}
	if err := deps.VM.Create(ctx, CreateRequest{CandidateID: in.CandidateID, DiskGiB: in.Recipe.Machine.SystemDiskGiB}); err != nil {
		return Result{}, fmt.Errorf("create candidate: %w", err)
	}
	created, err := deps.VM.Observe(ctx, in.CandidateID)
	if err != nil {
		return Result{}, fmt.Errorf("observe created candidate: %w", err)
	}
	if created.ObjectID != in.CandidateID || !created.Exists || created.State != backend.ObjectStopped {
		return Result{}, errors.New("created candidate is not the exact stopped object")
	}
	if err := setPhase(PhaseCreated); err != nil {
		return Result{}, err
	}
	if err := deps.VM.Configure(ctx, ConfigureRequest{CandidateID: in.CandidateID, CPUs: in.Recipe.Machine.CPUs, MemoryMiB: in.Recipe.Machine.MemoryMiB}); err != nil {
		return Result{}, fmt.Errorf("configure candidate: %w", err)
	}
	if err := setPhase(PhaseRunning); err != nil {
		return Result{}, err
	}
	handle, startErr := deps.VM.RunInstaller(ctx, InstallerRequest{CandidateID: in.CandidateID, RunID: in.RunID, ISOPath: installerISO, SerialDirectory: filepath.Join(attemptDir, "serial")})
	if handle == nil {
		return Result{}, errors.Join(errors.New("installer returned no owned handle"), startErr)
	}
	defer func() {
		if err == nil {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = errors.Join(err, handle.Stop(cleanupCtx), handle.Wait(cleanupCtx))
	}()
	if startErr != nil {
		return Result{}, fmt.Errorf("start installer: %w", startErr)
	}
	installCtx, cancel := context.WithTimeout(ctx, 90*time.Minute)
	defer cancel()
	if err := handle.WaitFor(installCtx, InstalledPrompt(in.RunID)); err != nil {
		return Result{}, fmt.Errorf("wait for installed guest: %w", err)
	}
	if err := setPhase(PhaseFinalizing); err != nil {
		return Result{}, err
	}
	finalCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := handle.SendLine(finalCtx, FinalizerCommand); err != nil {
		return Result{}, fmt.Errorf("send fixed finalizer command: %w", err)
	}
	if err := handle.WaitFor(finalCtx, CloneReadyMarker); err != nil {
		return Result{}, fmt.Errorf("wait for clone-ready marker: %w", err)
	}
	if err := setPhase(PhaseStopping); err != nil {
		return Result{}, err
	}
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	if err := handle.SendLine(shutdownCtx, PoweroffCommand); err != nil {
		return Result{}, fmt.Errorf("send fixed poweroff command: %w", err)
	}
	if err := handle.Wait(shutdownCtx); err != nil {
		return Result{}, fmt.Errorf("wait for installer shutdown: %w", err)
	}
	stopped, err := deps.VM.Observe(ctx, in.CandidateID)
	if err != nil {
		return Result{}, fmt.Errorf("observe stopped candidate: %w", err)
	}
	if stopped.ObjectID != in.CandidateID || !stopped.Exists || stopped.State != backend.ObjectStopped {
		return Result{}, errors.New("candidate did not stop as the exact owned object")
	}
	if err := setPhase(PhaseCandidateStopped); err != nil {
		return Result{}, err
	}
	return Result{AttemptDirectory: attemptDir, CandidateID: in.CandidateID, PreparationKey: key, State: CandidateStopped}, nil
}

func validate(in Inputs, deps Dependencies) error {
	if deps.Checks == nil {
		return errors.New("input checks are required")
	}
	if deps.Seed == nil {
		return errors.New("seed builder and private verifier provider are required")
	}
	if deps.VM == nil {
		return errors.New("candidate VM adapter is required")
	}
	if !validRunID(in.RunID) {
		return errors.New("unsupported guest build run ID")
	}
	if err := backend.ValidateObjectID(in.CandidateID); err != nil {
		return err
	}
	if err := backend.ValidateObjectID(in.AttemptID); err != nil {
		return err
	}
	if !absoluteClean(in.AttemptRoot) || !absoluteClean(in.ISOPath) || !absoluteClean(in.GuestDefinitionRoot) {
		return errors.New("build paths must be canonical and absolute")
	}
	root, err := os.Lstat(in.AttemptRoot)
	if err != nil {
		return err
	}
	if !root.IsDir() || root.Mode().Perm() != 0700 {
		return errors.New("attempt root must be a private directory")
	}
	resolved, err := filepath.EvalSymlinks(in.AttemptRoot)
	if err != nil {
		return err
	}
	if resolved != in.AttemptRoot {
		return errors.New("attempt root has a symlinked ancestor")
	}
	return nil
}

func absoluteClean(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsAny(path, "\r\n\x00")
}

func validateVerifier(attemptDir, path string) error {
	if !privateVerifierPath(attemptDir, path) {
		return errors.New("builder verifier must be directly inside the private attempt")
	}
	entry, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect builder verifier: %w", err)
	}
	if !entry.Mode().IsRegular() || entry.Mode().Perm() != 0600 {
		return errors.New("builder verifier must be a regular private file")
	}
	stat, ok := entry.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		return errors.New("builder verifier must be owned by the operator with one link")
	}
	return nil
}

func privateVerifierPath(attemptDir, path string) bool {
	return absoluteClean(path) && filepath.Dir(path) == attemptDir
}

func writeAttempt(dir string, state Attempt) error {
	return writeAttemptWithHook(dir, state, nil)
}

func writeAttemptWithHook(dir string, state Attempt, afterCreate func() error) (retErr error) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temporary := filepath.Join(dir, "attempt.json.next")
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	temporaryOwned := true
	defer func() {
		if temporaryOwned {
			if cleanupErr := os.Remove(temporary); cleanupErr != nil && !os.IsNotExist(cleanupErr) {
				retErr = errors.Join(retErr, fmt.Errorf("remove failed attempt journal temporary: %w", cleanupErr))
			}
		}
	}()
	if afterCreate != nil {
		if err := afterCreate(); err != nil {
			return errors.Join(err, file.Close())
		}
	}
	_, writeErr := file.Write(data)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(dir, "attempt.json")); err != nil {
		return err
	}
	temporaryOwned = false
	opened, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer opened.Close()
	return opened.Sync()
}

func ReadAttempt(dir string) (Attempt, error) {
	if !absoluteClean(dir) {
		return Attempt{}, errors.New("attempt directory must be canonical and absolute")
	}
	if err := privateStateRoot(dir); err != nil {
		return Attempt{}, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Attempt{}, err
	}
	defer root.Close()
	entry, err := root.Lstat("attempt.json")
	if err != nil {
		return Attempt{}, err
	}
	if err := privateRegular(filepath.Join(dir, "attempt.json"), entry); err != nil {
		return Attempt{}, err
	}
	file, err := root.OpenFile("attempt.json", os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return Attempt{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(entry, opened) {
		return Attempt{}, errors.New("attempt journal changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return Attempt{}, err
	}
	if len(data) > 4096 || int64(len(data)) != entry.Size() {
		return Attempt{}, errors.New("attempt journal exceeds limit or changed while reading")
	}
	var state Attempt
	if err := json.Unmarshal(data, &state); err != nil {
		return Attempt{}, err
	}
	if state.Version != 1 {
		return Attempt{}, errors.New("unsupported attempt state version")
	}
	return state, nil
}
