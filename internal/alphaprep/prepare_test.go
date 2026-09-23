package alphaprep

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/hostx"
)

type fixedRuntimeDoctor struct {
	runtime hostx.RuntimeExpectation
	calls   int
}

func (d *fixedRuntimeDoctor) CheckRuntime(context.Context, hostx.Request) (hostx.RuntimeExpectation, error) {
	d.calls++
	return d.runtime, nil
}

func TestPrepareRequiresExactDomainOwnedAttemptRootBeforePreflight(t *testing.T) {
	loaded, selected := preflightFixture(t)
	doctor, ca := &recordingDoctor{}, &recordingCA{}
	request := basebuild.PrepareRequest{StateRoot: selected.StateRoot, Inputs: basebuild.Inputs{AttemptRoot: filepath.Join(filepath.Dir(selected.StateRoot), "personal", "prepared-attempts")}}
	if _, err := Prepare(context.Background(), loaded, selected, "/qualified/config.json", request, doctor, ca, BuildComponents{}); err == nil || doctor.calls != 0 || ca.calls != 0 {
		t.Fatalf("cross-domain attempt path reached preflight: %v, doctor=%d CA=%d", err, doctor.calls, ca.calls)
	}
	request.Inputs.AttemptRoot = filepath.Join(selected.StateRoot, "prepared-attempts")
	request.StateRoot += "-other"
	if _, err := Prepare(context.Background(), loaded, selected, "/qualified/config.json", request, doctor, ca, BuildComponents{}); err == nil || doctor.calls != 0 || ca.calls != 0 {
		t.Fatalf("cross-domain state root reached preflight: %v, doctor=%d CA=%d", err, doctor.calls, ca.calls)
	}
}

func TestPrepareHostPreflightFailureDoesNotCreateAttemptRoot(t *testing.T) {
	loaded, selected := preflightFixture(t)
	doctor, ca := &recordingDoctor{err: errors.New("host drift")}, &recordingCA{}
	attemptRoot := filepath.Join(selected.StateRoot, "prepared-attempts")
	request := basebuild.PrepareRequest{StateRoot: selected.StateRoot, Inputs: basebuild.Inputs{AttemptRoot: attemptRoot}}
	if _, err := Prepare(context.Background(), loaded, selected, "/qualified/config.json", request, doctor, ca, BuildComponents{}); err == nil || doctor.calls != 1 || ca.calls != 0 {
		t.Fatalf("host drift reached mutation: %v, doctor=%d CA=%d", err, doctor.calls, ca.calls)
	}
	if _, err := os.Lstat(attemptRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight failure created attempt root: %v", err)
	}
}

func TestEnsureAttemptRootCreatesPrivateDirectoryAndRejectsReplacement(t *testing.T) {
	_, selected := preflightFixture(t)
	attemptRoot := filepath.Join(selected.StateRoot, "prepared-attempts")
	if err := ensureAttemptRoot(selected.StateRoot); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(attemptRoot)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("private attempt root = %v, %v", info, err)
	}
	if err := os.Chmod(attemptRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensureAttemptRoot(selected.StateRoot); err == nil {
		t.Fatal("world-readable attempt root admitted")
	}
	if err := os.Remove(attemptRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(selected.StateRoot), attemptRoot); err != nil {
		t.Fatal(err)
	}
	if err := ensureAttemptRoot(selected.StateRoot); err == nil {
		t.Fatal("symlinked attempt root admitted")
	}
}

func TestPrepareChecksExactSourceBeforeCreatingAttemptRoot(t *testing.T) {
	loaded, selected := preflightFixture(t)
	components, runtime := buildComponentsFixture(t)
	host, err := loaded.HostAdmission()
	if err != nil {
		t.Fatal(err)
	}
	runtime.Manifest.Tart.Path = host.Host.TartExecutable
	if err := os.Chmod(host.Host.TartExecutable, 0700); err != nil {
		t.Fatal(err)
	}
	tartBytes, err := os.ReadFile(host.Host.TartExecutable)
	if err != nil {
		t.Fatal(err)
	}
	runtime.Manifest.Tart.ExecutableSHA256 = fmt.Sprintf("%x", sha256.Sum256(tartBytes))
	runtime.Manifest.TartHome = host.Host.TartHome
	doctor, ca := &fixedRuntimeDoctor{runtime: runtime}, &recordingCA{}
	attemptRoot := filepath.Join(selected.StateRoot, "prepared-attempts")
	request := basebuild.PrepareRequest{StateRoot: selected.StateRoot, Inputs: basebuild.Inputs{AttemptRoot: attemptRoot, ISOPath: filepath.Join(selected.StateRoot, "missing.iso")}}
	if _, err := Prepare(context.Background(), loaded, selected, "/qualified/config.json", request, doctor, ca, components); err == nil || doctor.calls != 1 || ca.calls != 1 {
		t.Fatalf("invalid source was not checked after admission: %v, doctor=%d CA=%d", err, doctor.calls, ca.calls)
	}
	if _, err := os.Lstat(attemptRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source verification failure created attempt root: %v", err)
	}
}
