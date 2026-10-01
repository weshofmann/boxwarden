//go:build n1clipboarddiagnostic && !n1candidate

package adjudicate

import "github.com/weshofmann/boxwarden/internal/clipboarddiag"

type ClipboardResult struct {
	Version          int    `json:"version"`
	Outcome          string `json:"outcome"`
	Equal            bool   `json:"equal"`
	MetadataComplete bool   `json:"metadata_complete"`
	Layer            string `json:"layer"`
}

func Clipboard(i clipboarddiag.InvocationReceipt, c clipboarddiag.CollectionReceipt) ClipboardResult {
	r := ClipboardResult{Version: 1, Outcome: "unknown", Layer: "metadata_incomplete"}
	if i.Validate() != nil {
		return r
	}
	r.Outcome = i.Outcome
	r.Equal = i.Synthetic.Equal && i.Outcome == "committed" && i.Synthetic.Length == len("boxwarden-n1-clipboard-control\n")
	if c.Validate() != nil || c.Binding != i.Binding {
		return r
	}
	r.MetadataComplete = c.Complete
	if i.Outcome == "unknown" {
		r.Layer = "acknowledgement_unknown"
		return r
	}
	if r.Equal && c.Complete {
		r.Layer = "synthetic_verified"
		return r
	}
	// Preserve only fixed producer stage/status codes. Later missing coverage
	// cannot turn a first observed failure into a completed transfer.
	for _, f := range c.Fragments {
		for _, e := range f.Records {
			if e.Status != "ok" {
				r.Layer = e.Stage
				return r
			}
		}
	}
	if c.Overlay != nil {
		for _, e := range c.Overlay.Records {
			switch e.Event {
			case "session_failed", "claim_refused", "claim_failed", "read_failed", "text_absent", "text_invalid", "native_read_absent", "native_read_invalid", "native_read_failed", "frame_error", "frame_unknown", "frame_failed", "ack_error", "ack_unknown", "ack_failed", "trace_saturated", "metadata_unavailable":
				r.Layer = e.Event
				return r
			}
		}
	}
	return r
}
