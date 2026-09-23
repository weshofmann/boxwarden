package basebuild

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

type changedOwnerInfo struct {
	os.FileInfo
	stat syscall.Stat_t
}

func (f changedOwnerInfo) Sys() any { return &f.stat }

func TestPreparedMetadataRequiresOperatorOwner(t *testing.T) {
	path := t.TempDir()
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatal(err)
	}
	entry, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat := *entry.Sys().(*syscall.Stat_t)
	stat.Uid++
	if err := privateDirectory(path, changedOwnerInfo{entry, stat}); err == nil {
		t.Fatal("foreign-owned prepared directory admitted")
	}
	file := filepath.Join(path, "record")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	entry, err = os.Lstat(file)
	if err != nil {
		t.Fatal(err)
	}
	stat = *entry.Sys().(*syscall.Stat_t)
	stat.Uid++
	if err := privateRegular(file, changedOwnerInfo{entry, stat}); err == nil {
		t.Fatal("foreign-owned prepared record admitted")
	}
}

type fakeQualifier struct {
	calls         int
	fail          bool
	badReceipt    bool
	emptyEvidence bool
}

func (f *fakeQualifier) Qualify(_ context.Context, candidate Result) (QualificationReceipt, error) {
	f.calls++
	if f.fail {
		return QualificationReceipt{}, errors.New("fresh clone failed")
	}
	evidence := []byte(`{"clone":"passed"}`)
	if f.emptyEvidence {
		evidence = nil
	}
	bom := []byte(`{"packages":[]}`)
	if err := os.WriteFile(filepath.Join(candidate.AttemptDirectory, "qualification-evidence.json"), evidence, 0600); err != nil {
		return QualificationReceipt{}, err
	}
	if err := os.WriteFile(filepath.Join(candidate.AttemptDirectory, "qualification-bom.json"), bom, 0600); err != nil {
		return QualificationReceipt{}, err
	}
	receipt := QualificationReceipt{
		CandidateID:    candidate.CandidateID,
		PreparationKey: candidate.PreparationKey,
		CloneID:        "bw-v02-qual-r1",
		EvidenceSHA256: fmt.Sprintf("%x", sha256.Sum256(evidence)),
		BOMSHA256:      fmt.Sprintf("%x", sha256.Sum256(bom)),
		Passed:         true,
	}
	if f.badReceipt {
		receipt.PreparationKey = strings.Repeat("d", 64)
	}
	return receipt, nil
}

func prepareFixture(t *testing.T) (PrepareRequest, PrepareDependencies, *fakeChecks, *fakeVM, *fakeQualifier) {
	t.Helper()
	in := exampleInputs(t)
	stateRoot, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stateRoot, 0700); err != nil {
		t.Fatal(err)
	}
	events := []string{}
	checks := &fakeChecks{}
	vm := &fakeVM{events: &events, run: &fakeRun{events: &events}}
	qualifier := &fakeQualifier{}
	deps := PrepareDependencies{Build: Dependencies{Checks: checks, Seed: fakeSeed{events: &events}, VM: vm}, Qualifier: qualifier}
	return PrepareRequest{StateRoot: stateRoot, Inputs: in}, deps, checks, vm, qualifier
}

func TestPrepareAdmitsOnlyQualifiedStoppedCandidateAndReusesIt(t *testing.T) {
	request, deps, checks, vm, qualifier := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition != PreparedBuilt || first.Record.CandidateID != request.Inputs.CandidateID || !first.Record.Qualification.Passed {
		t.Fatalf("first result: %+v", first)
	}
	if qualifier.calls != 1 {
		t.Fatalf("qualifier calls: %d", qualifier.calls)
	}
	if checks.called != 2 {
		t.Fatalf("fresh input verification calls: %d", checks.called)
	}
	state, err := ReadAttempt(filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID))
	if err != nil || state.Phase != PhaseAdmitted {
		t.Fatalf("attempt: %+v, %v", state, err)
	}

	second, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	if second.Disposition != PreparedReused || second.Record != first.Record {
		t.Fatalf("reused result: %+v", second)
	}
	if qualifier.calls != 1 {
		t.Fatalf("qualified again on cache hit: %d", qualifier.calls)
	}
	if checks.called != 3 {
		t.Fatalf("cache hit skipped input verification: %d", checks.called)
	}
	if err := os.WriteFile(filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID, "qualification-evidence.json"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("cache reused after qualification evidence changed")
	}
	if err := os.WriteFile(filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID, "qualification-evidence.json"), []byte(`{"clone":"passed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	vm.run = nil
	vm.state = backend.ObjectRunning
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("running cached base reused")
	}
}

func TestPrepareRejectsRecreatedCandidateWithSameName(t *testing.T) {
	request, deps, _, vm, qualifier := prepareFixture(t)
	if _, err := Prepare(context.Background(), request, deps); err != nil {
		t.Fatal(err)
	}
	vm.identity = strings.Repeat("c", 64)
	if _, err := Prepare(context.Background(), request, deps); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("recreated candidate reused: %v", err)
	}
	if qualifier.calls != 1 {
		t.Fatalf("recreated candidate triggered new qualification: %d", qualifier.calls)
	}
}

func TestPrepareRecoversDurablePendingAdmissionBeforePublication(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadAttempt(first.Record.AttemptDirectory)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = PhaseAdmissionPending
	if err := writeAttempt(first.Record.AttemptDirectory, state); err != nil {
		t.Fatal(err)
	}
	name := first.Record.PreparationKey + ".json"
	if err := os.Remove(filepath.Join(request.StateRoot, "prepared", "records", name)); err != nil {
		t.Fatal(err)
	}
	recovered, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Disposition != PreparedReused || recovered.Record != first.Record || qualifier.calls != 1 {
		t.Fatalf("pending admission rebuilt or requalified: %+v, calls %d", recovered, qualifier.calls)
	}
	state, err = ReadAttempt(first.Record.AttemptDirectory)
	if err != nil || state.Phase != PhaseAdmitted {
		t.Fatalf("recovered journal: %+v, %v", state, err)
	}
}

func TestPrepareRecoversInterruptedHardlinkPublication(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadAttempt(first.Record.AttemptDirectory)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = PhaseAdmissionPending
	if err := writeAttempt(first.Record.AttemptDirectory, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.StateRoot, "prepared", "records", first.Record.PreparationKey+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	interrupted := errors.New("interrupted after publication")
	if err := storePreparedWithHook(request.StateRoot, first.Record, func() error { return interrupted }); !errors.Is(err, interrupted) {
		t.Fatalf("publication interruption: %v", err)
	}
	recovered, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Disposition != PreparedReused || qualifier.calls != 1 {
		t.Fatalf("hardlink recovery rebuilt or requalified: %+v, calls %d", recovered, qualifier.calls)
	}
	if _, err := os.Lstat(path + ".next"); !os.IsNotExist(err) {
		t.Fatalf("staged hardlink retained: %v", err)
	}
	entry, err := os.Lstat(path)
	if err != nil || privateRegular(path, entry) != nil {
		t.Fatalf("published record remains unsafe: %v, %v", entry, err)
	}
}

func TestPrepareRecoversPartiallyWrittenUnpublishedRecord(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadAttempt(first.Record.AttemptDirectory)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = PhaseAdmissionPending
	if err := writeAttempt(first.Record.AttemptDirectory, state); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.StateRoot, "prepared", "records", first.Record.PreparationKey+".json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".next", []byte(`{"version":`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), request, deps); err != nil {
		t.Fatalf("partial scratch blocked durable admission recovery: %v", err)
	}
	if qualifier.calls != 1 {
		t.Fatalf("partial scratch triggered requalification: %d", qualifier.calls)
	}
}

func TestPrepareRecoversInterruptedFinalJournalWrite(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err := ReadAttempt(first.Record.AttemptDirectory)
	if err != nil {
		t.Fatal(err)
	}
	state.Phase = PhaseAdmissionPending
	if err := writeAttempt(first.Record.AttemptDirectory, state); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(first.Record.AttemptDirectory, "attempt.json.next")
	if err := os.WriteFile(staged, []byte(`{"phase":"admitted"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), request, deps); err != nil {
		t.Fatalf("stale attempt temporary blocked admission: %v", err)
	}
	if qualifier.calls != 1 {
		t.Fatalf("final journal interruption triggered requalification: %d", qualifier.calls)
	}
	if _, err := os.Lstat(staged); !os.IsNotExist(err) {
		t.Fatalf("stale attempt temporary retained: %v", err)
	}
	state, err = ReadAttempt(first.Record.AttemptDirectory)
	if err != nil || state.Phase != PhaseAdmitted {
		t.Fatalf("final journal not recovered: %+v, %v", state, err)
	}
}

func TestPrepareLeavesFailedQualificationUnadmitted(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	qualifier.fail = true
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("qualification failure accepted")
	}
	state, err := ReadAttempt(filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID))
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != PhaseFailed || state.Failure != "qualification failed" {
		t.Fatalf("attempt: %+v", state)
	}
	if _, err := os.Stat(filepath.Join(request.StateRoot, "prepared", "records", strings.Repeat("a", 64)+".json")); !os.IsNotExist(err) {
		t.Fatalf("failed qualification admitted: %v", err)
	}
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("failed candidate implicitly resumed")
	}
}

func TestPrepareRejectsEmptyQualificationEvidence(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	qualifier.emptyEvidence = true
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("empty qualification evidence admitted")
	}
	if _, err := os.Stat(filepath.Join(request.StateRoot, "prepared", "records", strings.Repeat("a", 64)+".json")); !os.IsNotExist(err) {
		t.Fatalf("empty evidence produced cache entry: %v", err)
	}
}

func TestPrepareRejectsMismatchedReceiptAndTamperedCache(t *testing.T) {
	request, deps, _, _, qualifier := prepareFixture(t)
	qualifier.badReceipt = true
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("mismatched receipt accepted")
	}
	if _, err := os.Stat(filepath.Join(request.StateRoot, "prepared", "records", strings.Repeat("a", 64)+".json")); !os.IsNotExist(err) {
		t.Fatalf("mismatched receipt admitted: %v", err)
	}

	request, deps, _, _, _ = prepareFixture(t)
	if _, err := Prepare(context.Background(), request, deps); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.StateRoot, "prepared", "records", strings.Repeat("a", 64)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, ' '), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("tampered cache reused")
	}
}

func TestPrepareRequiresQualifierBeforeAnyStateMutation(t *testing.T) {
	request, deps, _, _, _ := prepareFixture(t)
	deps.Qualifier = nil
	if _, err := Prepare(context.Background(), request, deps); err == nil {
		t.Fatal("missing qualifier accepted")
	}
	if _, err := os.Stat(filepath.Join(request.Inputs.AttemptRoot, request.Inputs.AttemptID)); !os.IsNotExist(err) {
		t.Fatalf("attempt created: %v", err)
	}
}

func TestPrepareRejectsFailedGuestSoftwareBeforeCacheAdmission(t *testing.T) {
	request, deps, _, vm, qualifier := prepareFixture(t)
	request.Inputs.Recipe.AptPackages = []string{"git"}
	request.Inputs.Recipe.Steps = []recipe.Step{{ID: "base-step", Phase: "prepare", Argv: []string{"/bin/true"}}}
	vm.run.failAt = "prepared"
	if _, err := Prepare(context.Background(), request, deps); err == nil || !strings.Contains(err.Error(), "guest preparation failed") {
		t.Fatalf("failed guest preparation was admitted: %v", err)
	}
	if qualifier.calls != 0 {
		t.Fatal("qualifier ran after guest preparation failure")
	}
	if _, err := os.Stat(filepath.Join(request.StateRoot, "prepared", "records", strings.Repeat("a", 64)+".json")); !os.IsNotExist(err) {
		t.Fatalf("failed guest preparation published cache: %v", err)
	}
}

func TestPrepareAdmitsGuestSoftwareOnlyAfterPrepareAndQualification(t *testing.T) {
	request, deps, _, vm, qualifier := prepareFixture(t)
	request.Inputs.Recipe.AptPackages = []string{"git"}
	request.Inputs.Recipe.Steps = []recipe.Step{{ID: "base-step", Phase: "prepare", Argv: []string{"/bin/true"}}}
	result, err := Prepare(context.Background(), request, deps)
	if err != nil || result.Disposition != PreparedBuilt || qualifier.calls != 1 {
		t.Fatalf("prepared recipe admission = %+v, qualifier calls=%d, err=%v", result, qualifier.calls, err)
	}
	joined := strings.Join(*vm.events, ",")
	if !strings.Contains(joined, "prepare-command,prepared,finalizer-command") {
		t.Fatalf("software prepare did not precede finalizer: %s", joined)
	}
}
