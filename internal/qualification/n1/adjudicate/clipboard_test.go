//go:build n1clipboarddiagnostic && !n1candidate

package adjudicate

import (
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"testing"
	"time"
)

func TestUnknownCannotBecomeAcknowledgedByMetadata(t *testing.T) {
	o := clipboarddiag.Operation{Version: 1, OperationID: "10000000-0000-0000-0000-000000000001", Direction: "write", Domain: "n1qualification", SessionID: "20000000-0000-0000-0000-000000000001", BackendKind: "tart", BackendObject: "fixed", Generation: "30000000-0000-0000-0000-000000000001", ExpiresAt: time.Unix(100, 0).UTC()}
	i := clipboarddiag.InvocationReceipt{Version: 1, Binding: o, Outcome: "unknown", Fragments: []clipboarddiag.Fragment{}, Synthetic: clipboarddiag.SyntheticResult{FixtureID: "n1_clipboard_control_v1", ExpectedSHA256: "9096af926f38bc69facaa4d383ba789f106a6506fadf6cedd170616b882f88e7", Equal: true}}
	c := clipboarddiag.CollectionReceipt{Version: 1, Binding: o, Fragments: []clipboarddiag.Fragment{}}
	if i.Validate() != nil || c.Validate() != nil {
		t.Fatal("fixture invalid")
	}
	r := Clipboard(i, c)
	if r.Outcome != "unknown" || r.Equal || r.Layer != "acknowledgement_unknown" {
		t.Fatal(r)
	}
	c.Binding.Generation = "40000000-0000-0000-0000-000000000001"
	r = Clipboard(i, c)
	if r.Layer != "metadata_incomplete" {
		t.Fatal(r)
	}
}
