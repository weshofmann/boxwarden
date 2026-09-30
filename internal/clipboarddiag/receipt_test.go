//go:build n1clipboarddiagnostic

package clipboarddiag

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFrozenPythonCollectorJointWireFixturesAndProgression(t *testing.T) {
	raw, err := os.ReadFile("testdata/task2-r1-receipts.json")
	if err != nil {
		t.Fatal(err)
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		t.Fatal("fixture malformed")
	}
	for _, raw := range values {
		var expected OverlayReceipt
		if StrictDecode(raw, &expected, 16384) != nil {
			t.Fatal("frozen Python schema rejected")
		}
		if _, err := DecodeOverlay(raw, expected.Binding); err != nil {
			t.Fatal("frozen Python positive rejected", err)
		}
		for _, event := range []string{"claim_begin", "ack_ok", "native_read_complete"} {
			if !bytes.Contains(raw, []byte(`"event":"`+event+`"`)) {
				continue
			}
			mutated := bytes.Replace(raw, []byte(`"event":"`+event+`"`), []byte(`"event":"owner_clear"`), 1)
			if _, err := DecodeOverlay(mutated, expected.Binding); err == nil {
				t.Fatal("presence-only progression accepted", event)
			}
		}
		if expected.Closure != nil {
			proof, _ := EncodeClosure(*expected.Closure)
			if _, err := DecodeClosure(proof, expected.Binding); err != nil {
				t.Fatal(err)
			}
			for _, bad := range [][]byte{append(append([]byte{}, proof...), 1), bytes.Replace(proof, []byte(`"version":1`), []byte(`"version":1,"extra":0`), 1), bytes.Replace(proof, []byte(`"elapsed_ms":`), []byte(`"elapsed_ms":true,"ignored":`), 1)} {
				if _, err := DecodeClosure(bad, expected.Binding); err == nil {
					t.Fatal("malformed proof accepted")
				}
			}
		}
	}
}
func TestMaximalAdmittedFragmentCatalogueUnder32768(t *testing.T) {
	op := fixtureOperation()
	op.BackendObject = strings.Repeat("z", 128)
	op.ExpiresAt = time.Date(2030, 12, 31, 23, 59, 59, 999999999, time.UTC)
	longestStage := ""
	for stage := range stages {
		if len(stage) > len(longestStage) {
			longestStage = stage
		}
	}
	makeFragment := func(origin string, n int) Fragment {
		f := Fragment{1, op, origin, false, []RecordEntry{}}
		f.Records = uniqueCatalogue(origin, n)
		return f
	}
	host, cli, guest := makeFragment("supervisor", 26), makeFragment("cli", 6), makeFragment("guest", 31)
	digest := HeaderDigest(op)
	overlay := OverlayReceipt{Version: 1, Binding: op, HeaderDigest: digest, CoverageScope: "collected_progression_prefix", OwnerCode: "live", Owner: &NativeOwner{PID: 0xffffffff, StartTicks: ^uint64(0), UID: 1000, State: "live"}, Records: []OverlayRecord{}, Closure: &Closure{1, digest, "write", 59999}}
	for i := 0; i < 64; i++ {
		lane := "parent"
		if i >= 32 {
			lane = "owner"
		}
		overlay.Records = append(overlay.Records, OverlayRecord{Event: "binding", AtMS: 59999, HeaderDigest: digest, Lane: lane, Digest: &digest})
	}
	receipt := CollectionReceipt{1, op, false, []Fragment{host, cli, guest}, &overlay}
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	overlayRaw, _ := json.Marshal(overlay)
	if len(overlayRaw)+1 > MaxOverlayBytes {
		t.Fatal("overlay schema maximum exceeded", len(overlayRaw)+1)
	}
	t.Log("maximal overlay bytes", len(overlayRaw)+1)
	raw, _ := json.Marshal(receipt)
	if len(raw)+1 > 32768 {
		t.Fatal("maximum exceeds frozen collection bound", len(raw)+1)
	}
	t.Log("maximal admitted operation/catalogue bytes including newline", len(raw)+1)
	// Conservative schema envelope: Python maximum14260 plus each max32 compact
	// stages, three full bindings and fixed structural catalogue must also fit.
	rawGuest, _ := json.Marshal(guest)
	rawHost, _ := json.Marshal(host)
	rawCLI, _ := json.Marshal(cli)
	header, _ := json.Marshal(op)
	maxEntry, _ := json.Marshal(RecordEntry{"supervisor", longestStage, "unavailable", 59999})
	// Replace all admitted entries by the largest field lengths; duplicates are
	// disallowed in actual receipts, so this deliberately overestimates them.
	baseGuest, _ := json.Marshal(makeFragment("guest", 0))
	baseHost, _ := json.Marshal(makeFragment("supervisor", 0))
	baseCLI, _ := json.Marshal(makeFragment("cli", 0))
	envelope := 14260 + len(baseGuest) + len(baseHost) + len(baseCLI) + 64*(len(maxEntry)+1) + len(header) + 512
	_, _, _ = rawGuest, rawHost, rawCLI
	if envelope > 32768 {
		t.Fatal("conservative field maximum exceeded", envelope)
	}
	t.Log("conservative schema envelope", envelope)
}
