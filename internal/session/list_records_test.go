package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/domain"
)

func TestListRecordsReturnsSortedValidatedDomainRecords(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []Name{"zeta", "alpha"} {
		record := Record{Version: 2, Domain: "work", Name: name, ID: "11111111-1111-4111-8111-111111111111", Mode: ModeClean, IntendedState: StateStopped,
			Backend: BackendRef{Kind: "tart", ObjectID: "bw-" + string(name)}, GoldenRevision: "golden-r1", Readiness: ReadinessRecord{Status: ReadinessNotReady}}
		if err := SaveRecord(root, "work", record); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListRecords(root, "work")
	if err != nil || len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zeta" {
		t.Fatalf("records=%+v error=%v", got, err)
	}
	if _, err := ListRecords(root, domain.ID("personal")); err == nil {
		t.Fatal("foreign domain records exposed")
	}
	if err := os.WriteFile(filepath.Join(root, "sessions", "unexpected"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ListRecords(root, "work"); err == nil {
		t.Fatal("unexpected registry entry accepted")
	}
}
