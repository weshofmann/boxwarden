package coordinator

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"testing"
)

func testWindow() contract.Window {
	return contract.Window{LockSHA: contract.SHA([]byte("lock")), ID: "10000000-0000-0000-0000-000000000001", StartedUnixNS: 1, ExpiresUnixNS: 1 + contract.WindowNS, ContinuousStartNS: 10, ContinuousLimitNS: 10 + contract.WindowNS}
}
func TestBudgetChargesOriginAndStickyRegression(t *testing.T) {
	w := testWindow()
	b, e := newBudget(w, clock.Reading{Wall: 101, Continuous: 210})
	if e != nil {
		t.Fatal(e)
	}
	if b.active != 200 || b.attendance != 200 {
		t.Fatal("pre-L time not charged to both")
	}
	if b.check(clock.Reading{Wall: 201, Continuous: 310}, false) != nil || b.active != 300 || b.attendance != 200 {
		t.Fatal("active classification")
	}
	if b.check(clock.Reading{Wall: 202, Continuous: 309}, true) == nil || b.check(clock.Reading{Wall: 203, Continuous: 311}, false) == nil {
		t.Fatal("regression not sticky")
	}
}
func TestLedgerCrashConsumesAttemptAndFreshSlot(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ledger")
	l, e := newLedger(p, testWindow())
	if e != nil {
		t.Fatal(e)
	}
	a, e := l.reserve("control-create")
	if e != nil || !contract.UUID(a.ID) {
		t.Fatal(e)
	}
	if _, e = l.reserve("control-create"); e == nil {
		t.Fatal("retry admitted")
	}
	if _, e = newLedger(p, testWindow()); e == nil {
		t.Fatal("crash resume admitted")
	}
	if _, e = l.reserve("candidate-create"); e != nil {
		t.Fatal(e)
	}
	if _, e = l.reserve("foreign-create"); e == nil {
		t.Fatal("third guest admitted")
	}
	if e = l.closeAttempt(a.ID); e != nil {
		t.Fatal(e)
	}
}
func TestArchiveImmutableReadbackAndCaps(t *testing.T) {
	root := t.TempDir()
	a, e := newArchive(filepath.Join(root, "archive"), testWindow())
	if e != nil {
		t.Fatal(e)
	}
	if e = a.write("clipboard-verdict.json", []byte(`{"verdict":"unknown"}`)); e != nil {
		t.Fatal(e)
	}
	if e = a.write("clipboard-verdict.json", []byte(`{}`)); e == nil {
		t.Fatal("overwrite")
	}
	if e = a.write("../secret", []byte(`{}`)); e == nil {
		t.Fatal("arbitrary path")
	}
	if e = a.write("network-verdict.json", make([]byte, contract.MaxReceiptBytes+1)); e == nil {
		t.Fatal("overflow")
	}
	sha, files, n, e := a.finalize(true, true)
	if e != nil || files != 2 || n == 0 || len(sha) != 64 {
		t.Fatal(sha, files, n, e)
	}
	raw, e := os.ReadFile(filepath.Join(root, "archive", contract.ArchiveName))
	if e != nil {
		t.Fatal(e)
	}
	m, e := contract.ParseArchive(raw)
	if e != nil || len(m.Entries) != 1 {
		t.Fatal(e)
	}
	if e = a.write("network-verdict.json", []byte(`{}`)); e == nil {
		t.Fatal("post-final dispatch")
	}
}
func TestArchiveUnknownSiblingRefusesWithoutReadingIt(t *testing.T) {
	root := t.TempDir()
	a, e := newArchive(filepath.Join(root, "archive"), testWindow())
	if e != nil {
		t.Fatal(e)
	}
	if a.write("network-verdict.json", []byte(`{}`)) != nil {
		t.Fatal("write")
	}
	if os.WriteFile(filepath.Join(a.root, "private-ca"), []byte("must never archive"), 0000) != nil {
		t.Fatal("fixture")
	}
	if _, _, _, e = a.finalize(true, true); e == nil {
		t.Fatal("unrecorded member accepted")
	}
}
