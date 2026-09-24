package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/fake"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

func TestWorkspaceImportRoutesExactAlphaBindingAndReportsTransferOnly(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	const volume = "00112233-4455-4677-8899-aabbccddeeff"
	const digest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const mount = "/home/boxwarden/workspaces/project"
	input := AlphaImportInput{SourcePath: "/private/source", VolumeID: volume, SessionName: "dev"}
	called := 0
	var output bytes.Buffer
	options := Options{Output: &output, AlphaImport: func(_ context.Context, actual config.Domain, received AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		called++
		if actual != selected || received.SourcePath != input.SourcePath || received.VolumeID != volume || received.SessionName != "dev" || received.Resume || !alphaCreateUUID(received.TransactionID) {
			t.Fatalf("import lost exact request: domain=%+v input=%+v", actual, received)
		}
		journal := workspacex.ImportJournal{ID: received.TransactionID, Domain: selected.ID, VolumeID: volume, SessionName: "dev", MountPath: mount,
			Phase: workspacex.ImportTransferring, SourceDigest: digest, FileCount: 1, TotalBytes: 7}
		return journal, supervisor.ImportResult{Digest: digest, FileCount: 1, TotalBytes: 7, RemotePath: mount + "/boxwarden-import-" + received.TransactionID}, nil
	}}
	args := []string{"--config", configPath, "--domain", "alpha", "workspace", "import", "--source", input.SourcePath, volume, "dev"}
	if err := Run(t.Context(), args, options); err != nil || called != 1 {
		t.Fatalf("import calls=%d output=%q error=%v", called, output.String(), err)
	}
	if !strings.Contains(output.String(), "import: readback-matched\njournal: transferring\n") || strings.Contains(output.String(), "verified") {
		t.Fatalf("import overstates result: %q", output.String())
	}
	for _, suffix := range [][]string{
		{"--source", "relative", volume, "dev"},
		{"--source", input.SourcePath, "bad-volume", "dev"},
		{"--source", input.SourcePath, volume, "bad/name"},
	} {
		if err := Run(t.Context(), append([]string{"--config", configPath, "--domain", "alpha", "workspace", "import"}, suffix...), options); err == nil || called != 1 {
			t.Fatalf("invalid import reached callback: %v; calls=%d", suffix, called)
		}
	}
	workConfig, _ := writeV2DomainFixture(t, "work")
	if err := Run(t.Context(), []string{"--config", workConfig, "--domain", "work", "workspace", "import", "--source", input.SourcePath, volume, "dev"}, options); err == nil || called != 1 {
		t.Fatalf("foreign domain reached importer: %v; calls=%d", err, called)
	}
}

func TestWorkspaceImportResumeKeepsTransactionAndRejectsFalseReceipt(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	const transaction = "10213243-5465-4768-899a-bbccddeeff00"
	const volume = "00112233-4455-4677-8899-aabbccddeeff"
	called := 0
	var output bytes.Buffer
	options := Options{Output: &output, AlphaImport: func(_ context.Context, actual config.Domain, received AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error) {
		called++
		if actual != selected || received.TransactionID != transaction || received.VolumeID != volume || received.SessionName != "dev" || received.SourcePath != "" || !received.Resume {
			t.Fatalf("resume lost exact binding: domain=%+v input=%+v", actual, received)
		}
		return workspacex.ImportJournal{ID: transaction, Domain: selected.ID, VolumeID: volume, SessionName: "dev", MountPath: "/home/boxwarden/workspaces/project",
			Phase: workspacex.ImportTransferring, SourceDigest: strings.Repeat("a", 64), FileCount: 1, TotalBytes: 7}, supervisor.ImportResult{Digest: strings.Repeat("b", 64), FileCount: 1, TotalBytes: 7}, nil
	}}
	args := []string{"--config", configPath, "--domain", "alpha", "workspace", "import", "resume", "--volume", volume, "--session", "dev", transaction}
	if err := Run(t.Context(), args, options); err == nil || !strings.Contains(err.Error(), "invalid transfer receipt") {
		t.Fatalf("false import receipt accepted: %v", err)
	}
	if called != 1 || output.String() != "transaction: "+transaction+"\n" {
		t.Fatalf("resume output/calls: %q %d", output.String(), called)
	}
	for _, suffix := range [][]string{
		{"--volume", volume, "--session", "dev", "invalid"},
		{"--volume", volume, "--session", "bad/name", transaction},
		{"--volume", "bad-volume", "--session", "dev", transaction},
	} {
		if err := Run(t.Context(), append([]string{"--config", configPath, "--domain", "alpha", "workspace", "import", "resume"}, suffix...), options); err == nil || called != 1 {
			t.Fatalf("invalid resume reached callback: %v; calls=%d", suffix, called)
		}
	}
}

func TestWorkspaceImportVerifyRoutesExactPublishedExportAndRejectsFalseResult(t *testing.T) {
	configPath, selected := writeV2DomainFixture(t, "alpha")
	const transaction = "10213243-5465-4768-899a-bbccddeeff00"
	const exportID = "21213243-5465-4768-899a-bbccddeeff00"
	const volume = "00112233-4455-4677-8899-aabbccddeeff"
	observer := fake.New(backend.Observation{ObjectID: "owned", Exists: true, State: backend.ObjectStopped})
	called := 0
	var output bytes.Buffer
	options := Options{Output: &output, Observer: observer,
		AlphaImportVerify: func(_ context.Context, actual config.Domain, input AlphaImportVerifyInput, actualObserver backend.Observer) (workspacex.ImportJournal, error) {
			called++
			if actual != selected || input.TransactionID != transaction || input.ExportID != exportID || actualObserver != observer {
				t.Fatalf("verify lost exact domain, export, or backend observation: %+v %+v", actual, input)
			}
			return workspacex.ImportJournal{ID: transaction, Domain: selected.ID, VolumeID: volume,
				Phase: workspacex.ImportVerified, ExportID: exportID}, nil
		}}
	args := []string{"--config", configPath, "--domain", "alpha", "workspace", "import", "verify", "--export", exportID, transaction}
	if err := Run(t.Context(), args, options); err != nil || called != 1 ||
		!strings.Contains(output.String(), "import: verified\nexport-transaction: "+exportID+"\n") {
		t.Fatalf("public import verify = calls %d, output %q, error %v", called, output.String(), err)
	}
	for _, suffix := range [][]string{
		{"--export", exportID, "invalid"},
		{"--export", "invalid", transaction},
		{"--export", transaction, transaction},
	} {
		if err := Run(t.Context(), append([]string{"--config", configPath, "--domain", "alpha", "workspace", "import", "verify"}, suffix...), options); err == nil || called != 1 {
			t.Fatalf("invalid verification reached callback: %v; calls=%d", suffix, called)
		}
	}
	workConfig, _ := writeV2DomainFixture(t, "work")
	if err := Run(t.Context(), []string{"--config", workConfig, "--domain", "work", "workspace", "import", "verify", "--export", exportID, transaction}, options); err == nil || called != 1 {
		t.Fatalf("foreign domain reached alpha verifier: %v; calls=%d", err, called)
	}
	options.AlphaImportVerify = func(context.Context, config.Domain, AlphaImportVerifyInput, backend.Observer) (workspacex.ImportJournal, error) {
		return workspacex.ImportJournal{ID: transaction, Domain: selected.ID, VolumeID: volume,
			Phase: workspacex.ImportTransferring}, nil
	}
	output.Reset()
	if err := Run(t.Context(), args, options); err == nil || strings.Contains(output.String(), "import: verified") {
		t.Fatalf("false verified result accepted: output %q error %v", output.String(), err)
	}
}
