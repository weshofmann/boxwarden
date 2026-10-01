package contract

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func fixture() Handoff {
	return Handoff{Budget: Budget{ClosedUnixNS: 101, ClosedContinuousNS: 201, ActiveSpentNS: 2}, PublicRecords: [2]PublicRecord{{PublicRecordNames[0], repeat("d"), "12345678-1234-1234-1234-123456789abc"}, {PublicRecordNames[1], repeat("e"), "12345678-1234-1234-1234-123456789abc"}}, Version: 1, Window: Window{LockSHA: repeat("a"), ID: "12345678-1234-1234-1234-123456789abc", StartedUnixNS: 100, ExpiresUnixNS: 1800000000100, ContinuousStartNS: 200, ContinuousLimitNS: 1800000000200}, Configs: [2]string{StockConfigSHA, CandidateConfigSHA}, Pair: [2]Peer{{HostPinSHA: repeat("7"), Role: "control", Session: "11111111-1111-1111-1111-111111111111", Generation: "22222222-2222-2222-2222-222222222222", Backend: "test-control", Reaped: true, Deleted: true}, {HostPinSHA: repeat("8"), Role: "candidate", Session: "33333333-3333-3333-3333-333333333333", Generation: "44444444-4444-4444-4444-444444444444", Backend: "test-candidate", Reaped: true, Deleted: true}}, Attempts: []Attempt{{ID: "55555555-5555-5555-5555-555555555555", Command: "control-copy", Closed: true}}, ArchiveSHA: repeat("b"), ArchiveBytes: 1024, ArchiveFiles: 1, DispatchClosed: true, RuntimeClean: true}
}
func repeat(s string) string { return string(bytes.Repeat([]byte(s), 64)) }
func TestHandoffRequiresClosedOriginalWindow(t *testing.T) {
	h := fixture()
	raw, _ := json.Marshal(h)
	if _, e := ParseHandoff(raw); e != nil {
		t.Fatalf("valid handoff: %v", e)
	}
	for _, change := range []func(*Handoff){func(h *Handoff) { h.RuntimeClean = false }, func(h *Handoff) { h.Pair[0].Reaped = false }, func(h *Handoff) { h.Attempts[0].Closed = false }, func(h *Handoff) { h.Configs[0] = repeat("0") }, func(h *Handoff) { h.Window.ExpiresUnixNS++ }} {
		x := fixture()
		change(&x)
		b, _ := json.Marshal(x)
		if _, e := ParseHandoff(b); e == nil {
			t.Fatal("unproven handoff accepted")
		}
	}
}
func TestStrictBoundedCanonicalParsing(t *testing.T) {
	raw, _ := json.Marshal(fixture())
	for name, b := range map[string][]byte{"duplicate": bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), "unknown": bytes.Replace(raw, []byte(`"version":1`), []byte(`"extra":1,"version":1`), 1), "trailing": append(append([]byte{}, raw...), []byte(`{}`)...), "noncanonical": append([]byte(" "), raw...), "null": bytes.Replace(raw, []byte(`"runtime_clean":true`), []byte(`"runtime_clean":null`), 1), "oversize": bytes.Repeat([]byte(" "), MaxReceiptBytes+1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := ParseHandoff(b); e == nil {
				t.Fatal("malformed accepted")
			}
		})
	}
}
func TestCompletionNeedsActualExitAndOriginalBinding(t *testing.T) {
	h := fixture()
	c := Completion{Version: 1, Window: h.Window, HandoffSHA: repeat("c"), ArchiveSHA: h.ArchiveSHA, SoftnetSHA: SoftnetSHA, Removed: 3, DirectoryRemoved: true, ParentSynced: true, HandlesClosed: true}
	if e := ValidateCompletion(c, h, repeat("c"), 0, true, 101, 201); e != nil {
		t.Fatal(e)
	}
	for _, test := range []struct {
		exit       int
		eof        bool
		wall, mono uint64
	}{{1, true, 101, 201}, {0, false, 101, 201}, {0, true, h.Window.ExpiresUnixNS, 201}, {0, true, 99, 201}, {0, true, 101, h.Window.ContinuousLimitNS}, {0, true, 101, 199}} {
		if ValidateCompletion(c, h, repeat("c"), test.exit, test.eof, test.wall, test.mono) == nil {
			t.Fatal("lost exit/expiry accepted")
		}
	}
	_ = time.Second
}

func TestOnceOnlyLedgerAndOriginalCumulativeBudget(t *testing.T) {
	h := fixture()
	a := h.Attempts[0]
	a.ID = "66666666-6666-6666-6666-666666666666"
	h.Attempts = append(h.Attempts, a)
	raw, _ := Encode(h)
	if _, e := ParseHandoff(raw); e == nil {
		t.Fatal("duplicate command reset one-use ledger")
	}
	h = fixture()
	h.Budget.ActiveSpentNS = 15 * 60 * 1000000000
	h.Budget.AttendanceSpentNS = 6 * 60 * 1000000000
	h.Budget.ClosedUnixNS = h.Window.StartedUnixNS + 21*60*1000000000
	h.Budget.ClosedContinuousNS = h.Window.ContinuousStartNS + 21*60*1000000000
	if !h.Valid() {
		t.Fatal("21-minute handoff rejected despite remaining budgets")
	}
	if h.Check(h.Budget.ClosedUnixNS+3*60*1000000000, h.Budget.ClosedContinuousNS+3*60*1000000000) != nil {
		t.Fatal("remaining3min refused")
	}
	if h.Check(h.Budget.ClosedUnixNS+4*60*1000000000, h.Budget.ClosedContinuousNS+4*60*1000000000) == nil {
		t.Fatal("attendance cap reset")
	}
	h.Budget.AttendanceSpentNS = 9 * 60 * 1000000000
	h.Budget.ActiveSpentNS = 10 * 60 * 1000000000
	h.Budget.ClosedUnixNS = h.Window.StartedUnixNS + 19*60*1000000000
	h.Budget.ClosedContinuousNS = h.Window.ContinuousStartNS + 19*60*1000000000
	if h.Check(h.Budget.ClosedUnixNS+2*60*1000000000, h.Budget.ClosedContinuousNS+2*60*1000000000) == nil {
		t.Fatal("9 minutes prior attendance plus2 passed")
	}
	h.Budget.ActiveSpentNS = 0
	if h.Valid() {
		t.Fatal("unaccounted original elapsed admitted")
	}
}
func TestStaticLockNoMissingPlaceholderOrDuplicateAndWitnessBindings(t *testing.T) {
	s := StaticLock{Version: 1, Configs: [2]string{StockConfigSHA, CandidateConfigSHA}, SoftnetSHA: SoftnetSHA, CatalogueSHA: repeat("a"), SchemaSHA: repeat("b"), ProcedureSHA: repeat("c"), NoReplacement: true, ActiveSeconds: 1200, AttendanceSeconds: 600}
	for i := range s.Artifacts {
		s.Artifacts[i] = Artifact{Name: artifactNames[i], SHA: repeat(string(byte('1' + i))), Source: string(bytes.Repeat([]byte("a"), 40))}
	}
	raw, _ := Encode(s)
	if _, e := ParseStaticLock(raw); e != nil {
		t.Fatal(e)
	}
	s.Artifacts[4].SHA = repeat("0")
	raw, _ = Encode(s)
	if _, e := ParseStaticLock(raw); e == nil {
		t.Fatal("zero placeholder admitted")
	}
	w := Witness{Version: 1, Phase: 1, LockSHA: repeat("a"), WindowID: fixture().Window.ID, HandoffSHA: repeat("b"), Exit: 20}
	raw, e := EncodeWitness(w)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ParseWitness(raw); e != nil {
		t.Fatal(e)
	}
	if _, e = ParseWitness(bytes.TrimSuffix(raw, []byte("\n"))); e == nil {
		t.Fatal("noncanonical pipe frame admitted")
	}
	w.Phase = 2
	if _, e = EncodeWitness(w); e == nil {
		t.Fatal("unproven actual exit/completion admitted")
	}
}
