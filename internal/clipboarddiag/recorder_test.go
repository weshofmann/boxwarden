//go:build n1clipboarddiagnostic

package clipboarddiag

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"
)

func fixtureOperation() Operation {
	return Operation{Version: 1, OperationID: "00000000-0000-4000-8000-000000000001", Domain: "n1qualification", SessionID: "00000000-0000-4000-8000-000000000002", BackendKind: "tart", BackendObject: "synthetic", Generation: "00000000-0000-4000-8000-000000000003", Direction: "write", ExpiresAt: time.Now().UTC().Add(20 * time.Second)}
}
func TestOperationStrictBindingAndPythonCanonicalHeader(t *testing.T) {
	op := fixtureOperation()
	op.ExpiresAt = time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC)
	raw, err := EncodeOperation(op)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"backend_kind":"tart","backend_object":"synthetic","direction":"write","domain":"n1qualification","expires_at":"2030-01-02T03:04:05.123456789Z","generation":"00000000-0000-4000-8000-000000000003","operation_id":"00000000-0000-4000-8000-000000000001","session_id":"00000000-0000-4000-8000-000000000002","version":1}`
	if string(raw) != want {
		t.Fatalf("joint binding wire mismatch: %s", raw)
	}
	for _, bad := range [][]byte{bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":true`), 1), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"extra":0`), 1), append(append([]byte{}, raw...), raw...), bytes.Repeat([]byte("x"), 4097)} {
		if _, err := DecodeOperation(bad, false); err == nil {
			t.Fatal("invalid binding accepted")
		}
	}
}
func TestRecorderOverflowAndClockLossAreIncomplete(t *testing.T) {
	op := fixtureOperation()
	origin := time.Now()
	now := origin
	r, err := NewRecorder(op, "supervisor", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx, err := WithContext(context.Background(), op, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range uniqueCatalogue("supervisor", 32) {
		Record(WithSource(ctx, entry.Source), entry.Stage, "ok")
	}
	if !Available(ctx) {
		t.Fatal("bounded recorder unexpectedly lost")
	}
	Record(WithSource(ctx, "ssh"), "ssh_dispatch", "ok")
	if Available(ctx) || len(r.Fragment().Records) != 32 {
		t.Fatal("overflow silently passed")
	}
	r, _ = NewRecorder(op, "supervisor", func() time.Time { return now })
	ctx, _ = WithContext(context.Background(), op, r)
	now = origin.Add(-time.Nanosecond)
	Record(WithSource(ctx, "ssh"), "ssh_dispatch", "ok")
	if Available(ctx) {
		t.Fatal("regressing origin passed")
	}
}
func TestRecorderRejectsForeignAndUnknownAndDoesNotExposeArbitraryData(t *testing.T) {
	op := fixtureOperation()
	r, _ := NewRecorder(op, "supervisor", time.Now)
	foreign := op
	foreign.Generation = "00000000-0000-4000-8000-000000000004"
	if _, err := WithContext(context.Background(), foreign, r); err == nil {
		t.Fatal("foreign recorder bound")
	}
	ctx, _ := WithContext(context.Background(), op, r)
	Record(ctx, "secret-payload", "ok")
	raw, _ := json.Marshal(r.Fragment())
	if bytes.Contains(raw, []byte("secret-payload")) || r.Fragment().Complete {
		t.Fatal("arbitrary stage admitted")
	}
}
func TestStrictReceiptRejectsMissingNullBooleanAndNestedDuplicate(t *testing.T) {
	op := fixtureOperation()
	r, _ := NewRecorder(op, "cli", time.Now)
	ctx, _ := WithContext(t.Context(), op, r)
	Record(ctx, "cli_binding", "ok")
	Record(ctx, "cli_outcome", "unknown")
	raw, _ := json.Marshal(r.Fragment())
	for _, bad := range [][]byte{bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":null`), 1), bytes.Replace(raw, []byte(`"complete":true,`), nil, 1), bytes.Replace(raw, []byte(`"complete":true`), []byte(`"complete":true,"complete":false`), 1), bytes.Replace(raw, []byte(`"records":[`), []byte(`"records":null,"discard":[`), 1)} {
		var got Fragment
		if StrictDecode(bad, &got, MaxFragmentBytes) == nil {
			t.Fatal("missing/null/duplicate field accepted")
		}
	}
}
func TestHostCLIMergeRequiresCatalogueAndCombinedQuota(t *testing.T) {
	op := fixtureOperation()
	host, _ := NewRecorder(op, "supervisor", time.Now)
	cli, _ := NewRecorder(op, "cli", time.Now)
	hctx, _ := WithContext(context.Background(), op, host)
	cctx, _ := WithContext(context.Background(), op, cli)
	for _, entry := range uniqueCatalogue("supervisor", 31) {
		Record(WithSource(hctx, entry.Source), entry.Stage, "ok")
	}
	Record(cctx, "cli_binding", "ok")
	Record(cctx, "cli_outcome", "unknown")
	if _, ok := MergeHost(op, host.Fragment(), cli.Fragment()); ok {
		t.Fatal("combined32 quota ignored")
	}
	if _, ok := MergeHost(op, host.Fragment(), host.Fragment()); ok {
		t.Fatal("duplicate origin passed")
	}
	if _, ok := MergeHost(op, host.Fragment()); ok {
		t.Fatal("missing CLI fragment promoted")
	}
}

func TestIncompleteCollectionStillEnforcesCombinedHostQuota(t *testing.T) {
	op := fixtureOperation()
	host, _ := NewRecorder(op, "supervisor", time.Now)
	cli, _ := NewRecorder(op, "cli", time.Now)
	hctx, _ := WithContext(context.Background(), op, host)
	cctx, _ := WithContext(context.Background(), op, cli)
	for _, entry := range uniqueCatalogue("supervisor", 31) {
		Record(WithSource(hctx, entry.Source), entry.Stage, "ok")
	}
	Record(cctx, "cli_binding", "ok")
	Record(cctx, "cli_outcome", "unknown")
	receipt := CollectionReceipt{Version: 1, Binding: op, Fragments: []Fragment{host.Fragment(), cli.Fragment()}}
	if receipt.Validate() == nil {
		t.Fatal("incomplete bit bypassed joint32 quota")
	}
	fragments, complete := MergeHost(op, host.Fragment(), cli.Fragment())
	if complete || len(fragments) != 0 {
		t.Fatal("overflowing catalogue retained")
	}
}

func TestPublicationRejectsDuplicateClassificationAndMissingTerminal(t *testing.T) {
	op := fixtureOperation()
	empty := Fragment{Version: 1, Binding: op, Origin: "cli", Complete: true, Records: []RecordEntry{}}
	if empty.Validate() == nil {
		t.Fatal("complete empty publication admitted")
	}
	duplicate := Fragment{Version: 1, Binding: op, Origin: "cli", Records: []RecordEntry{{Source: "cli", Stage: "cli_outcome", Status: "unknown"}, {Source: "cli", Stage: "cli_outcome", Status: "ok"}}}
	if duplicate.Validate() == nil {
		t.Fatal("duplicate classification admitted")
	}
	host := empty
	host.Origin = "supervisor"
	if _, complete := MergeHost(op, host, empty); complete {
		t.Fatal("empty catalogues promoted")
	}
}
func TestRecorderAvailabilityIsSeparateFromTerminalCoverage(t *testing.T) {
	op := fixtureOperation()
	r, _ := NewRecorder(op, "cli", time.Now)
	ctx, _ := WithContext(t.Context(), op, r)
	if !Available(ctx) || r.Fragment().Complete {
		t.Fatal("initial recording availability/coverage confused")
	}
	Record(ctx, "cli_binding", "ok")
	if !Available(ctx) || r.Fragment().Complete {
		t.Fatal("partial publication promoted")
	}
	Record(ctx, "cli_outcome", "unknown")
	if !r.Fragment().Complete {
		t.Fatal("unknown outcome cannot have complete metadata")
	}
	Record(ctx, "cli_outcome", "ok")
	if Available(ctx) || r.Fragment().Complete || len(r.Fragment().Records) != 2 {
		t.Fatal("duplicate classification overwritten or appended")
	}
}

func uniqueCatalogue(origin string, n int) []RecordEntry {
	result := []RecordEntry{}
	keys := []string{}
	for source := range producerStages {
		keys = append(keys, source)
	}
	sort.Strings(keys)
	for _, source := range keys {
		if !originSource(origin, source) {
			continue
		}
		catalogue := append([]string{}, producerStages[source]...)
		sort.Strings(catalogue)
		for _, stage := range catalogue {
			if stage == "control_final_ok_frame" {
				continue
			}
			result = append(result, RecordEntry{Source: source, Stage: stage, Status: "unavailable", AtMS: 59999})
		}
	}
	if n > len(result) {
		panic("fixture exceeds producer catalogue")
	}
	return result[:n]
}

func TestDiagnosticProducerStagePairRejectsAcrossPublication(t *testing.T) {
	op := fixtureOperation()
	cli := Fragment{1, op, "cli", true, []RecordEntry{{"cli", "cli_binding", "ok", 0}, {"cli", "cli_outcome", "unknown", 0}}}
	for _, complete := range []bool{false, true} {
		host := Fragment{1, op, "supervisor", complete, []RecordEntry{{"runtime", "guest_complete", "ok", 0}, {"supervisor", "control_final_error_frame", "ok", 0}}}
		if host.Validate() == nil {
			t.Errorf("wrong producer admitted complete=%v", complete)
		}
		if fs, _ := MergeHost(op, host, cli); len(fs) != 0 {
			t.Errorf("merge retained wrong producer complete=%v", complete)
		}
		if (CollectionReceipt{Version: 1, Binding: op, Fragments: []Fragment{host, cli}}).Validate() == nil {
			t.Errorf("collection admitted wrong producer complete=%v", complete)
		}
		badCLI := cli
		badCLI.Complete = complete
		badCLI.Records = append([]RecordEntry{}, cli.Records[:1]...)
		badCLI.Records = append(badCLI.Records, RecordEntry{"cli", "guest_complete", "ok", 0}, cli.Records[1])
		if badCLI.Validate() == nil {
			t.Errorf("CLI forged guest stage complete=%v", complete)
		}
	}
	r, _ := NewRecorder(op, "supervisor", time.Now)
	ctx, _ := WithContext(t.Context(), op, r)
	Record(WithSource(ctx, "runtime"), "guest_complete", "ok")
	if Available(ctx) || len(r.Fragment().Records) != 0 {
		t.Fatal("recorder admitted wrong producer")
	}
}

func TestDiagnosticSupervisorTerminalRequiresSuccessfulSingleFinalization(t *testing.T) {
	op := fixtureOperation()
	cli := Fragment{1, op, "cli", true, []RecordEntry{{"cli", "cli_binding", "ok", 0}, {"cli", "cli_outcome", "unknown", 0}}}
	cases := [][]RecordEntry{{{"supervisor", "control_final_error_frame", "incomplete", 0}}, {{"supervisor", "control_final_error_frame", "ok", 0}, {"supervisor", "control_final_ok_frame", "ok", 0}}, {{"supervisor", "control_final_ok_frame", "ok", 0}, {"supervisor", "control_final_error_frame", "ok", 0}}}
	for i, records := range cases {
		for _, complete := range []bool{false, true} {
			if i == 0 && !complete {
				continue
			}
			host := Fragment{1, op, "supervisor", complete, records}
			if host.Validate() == nil {
				t.Errorf("terminal case%d admitted complete=%v", i, complete)
			}
			if fs, _ := MergeHost(op, host, cli); len(fs) != 0 {
				t.Errorf("terminal case%d merge admitted", i)
			}
			if (CollectionReceipt{Version: 1, Binding: op, Fragments: []Fragment{host, cli}}).Validate() == nil {
				t.Errorf("terminal case%d collection admitted", i)
			}
		}
	}
	r, _ := NewRecorder(op, "supervisor", time.Now)
	ctx, _ := WithContext(t.Context(), op, r)
	Record(ctx, "control_final_error_frame", "ok")
	if !r.Fragment().Complete {
		t.Fatal("written error/unknown frame cannot be complete")
	}
	Record(ctx, "control_final_ok_frame", "ok")
	if Available(ctx) || r.Fragment().Complete || len(r.Fragment().Records) != 1 {
		t.Fatal("conflicting classifications recorded")
	}
}
