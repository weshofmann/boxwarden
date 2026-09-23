package alphaprep

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/hostx"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type recordingDoctor struct {
	request hostx.Request
	err     error
	calls   int
}

func (d *recordingDoctor) CheckRuntime(_ context.Context, request hostx.Request) (hostx.RuntimeExpectation, error) {
	d.calls++
	d.request = request
	return hostx.RuntimeExpectation{SoftnetBinDir: "/qualified/softnet"}, d.err
}

type recordingCA struct {
	current sshx.Domain
	all     []sshx.Domain
	err     error
	calls   int
}

func (c *recordingCA) Check(_ context.Context, current sshx.Domain, all []sshx.Domain) (sshx.CAIdentity, error) {
	c.calls++
	c.current, c.all = current, append([]sshx.Domain(nil), all...)
	return sshx.CAIdentity{Version: 1, Domain: current.ID, StateRoot: current.StateRoot}, c.err
}

func preflightFixture(t *testing.T) (config.Config, config.Domain) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"work", "personal", "tart-home"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"tart", "softnet"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "config.json")
	data := fmt.Sprintf(`{"version":2,"host":{"tart_executable":%q,"tart_home":%q,"softnet_source":%q},"domains":{"work":{"state_root":%q},"personal":{"state_root":%q}}}`,
		filepath.Join(root, "tart"), filepath.Join(root, "tart-home"), filepath.Join(root, "softnet"), filepath.Join(root, "work"), filepath.Join(root, "personal"))
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := loaded.Domain("work")
	if err != nil {
		t.Fatal(err)
	}
	return loaded, selected
}

func TestPreflightChecksAllAdmittedDomainsBeforeReturningRuntime(t *testing.T) {
	loaded, selected := preflightFixture(t)
	doctor, ca := &recordingDoctor{}, &recordingCA{}
	runtime, err := Preflight(context.Background(), loaded, selected, doctor, ca)
	if err != nil || runtime.SoftnetBinDir != "/qualified/softnet" {
		t.Fatalf("Preflight = %+v, %v", runtime, err)
	}
	if doctor.calls != 1 || len(doctor.request.ConfiguredStateRoots) != 2 || doctor.request.ConfiguredStateRoots[0] != filepath.Join(filepath.Dir(selected.StateRoot), "personal") || doctor.request.ConfiguredStateRoots[1] != selected.StateRoot {
		t.Fatalf("host doctor did not receive complete domain roots: %+v", doctor)
	}
	if ca.calls != 1 || ca.current != (sshx.Domain{ID: selected.ID, StateRoot: selected.StateRoot}) || len(ca.all) != 2 || ca.all[0].ID != "personal" || ca.all[1].ID != "work" {
		t.Fatalf("CA check did not receive exact selected and complete configured domains: %+v", ca)
	}
}

func TestPreflightStopsBeforeCACheckWhenHostDrifts(t *testing.T) {
	loaded, selected := preflightFixture(t)
	doctor, ca := &recordingDoctor{err: errors.New("drifted")}, &recordingCA{}
	if _, err := Preflight(context.Background(), loaded, selected, doctor, ca); err == nil || ca.calls != 0 {
		t.Fatalf("host drift reached CA check: err=%v calls=%d", err, ca.calls)
	}
}

func TestPreflightRejectsDomainSubstitutionAndMissingCA(t *testing.T) {
	loaded, selected := preflightFixture(t)
	doctor, ca := &recordingDoctor{}, &recordingCA{}
	selected.StateRoot += "-substituted"
	if _, err := Preflight(context.Background(), loaded, selected, doctor, ca); err == nil || doctor.calls != 0 || ca.calls != 0 {
		t.Fatalf("substituted domain reached external checks: %v, doctor=%d CA=%d", err, doctor.calls, ca.calls)
	}
	selected, _ = loaded.Domain("work")
	ca.err = errors.New("CA missing")
	if _, err := Preflight(context.Background(), loaded, selected, doctor, ca); err == nil {
		t.Fatal("missing CA admitted")
	}
}
