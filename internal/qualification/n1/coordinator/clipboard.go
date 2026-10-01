//go:build n1diagnostic && n1clipboarddiagnostic && !n1candidate

package coordinator

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/adjudicate"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"time"
)

type clipboardReceipt struct {
	Version        int                           `json:"version"`
	Result         adjudicate.ClipboardResult    `json:"result"`
	Operation      clipboarddiag.Operation       `json:"operation"`
	Fixture        clipboarddiag.SyntheticResult `json:"fixture"`
	HostRecords    int                           `json:"host_records"`
	OverlayRecords int                           `json:"overlay_records"`
}

func (e *engine) clipboard() error {
	results := [2][2]adjudicate.ClipboardResult{}
	for i := 0; i < 2; i++ {
		for j := 0; j < 2; j++ {
			results[i][j] = adjudicate.ClipboardResult{Version: 1, Outcome: "skipped", Layer: "prerequisite_unproved"}
		}
	}
	stockOK := true
	for i := 0; i < 2; i++ {
		if i == 1 && !stockOK {
			break
		}
		copyOK := false
		for j, direction := range []string{"write", "read"} {
			if j == 1 && !copyOK {
				break
			}
			name := role(i) + "-copy"
			if j == 1 {
				name = role(i) + "-read"
			}
			err := e.phase(name, false, func(ctx context.Context, id string) (any, int, error) {
				p := e.pair[i].Identity
				expiry := time.Now().Add(25 * time.Second)
				if deadlineOf(ctx).Before(expiry) {
					expiry = deadlineOf(ctx)
				}
				o := clipboarddiag.Operation{Version: 1, OperationID: id, Direction: direction, Domain: "n1qualification", SessionID: p.Session, BackendKind: "tart", BackendObject: p.Backend, Generation: p.Generation, ExpiresAt: expiry.UTC()}
				inv := clipboarddiag.InvocationReceipt{}
				collection := clipboarddiag.CollectionReceipt{}
				req := struct {
					Version   int                     `json:"version"`
					Session   string                  `json:"session"`
					Operation clipboarddiag.Operation `json:"operation"`
				}{1, roleName(i), o}
				raw, _ := json.Marshal(req)
				if e.executable(i) != nil {
					return nil, 0, ErrRefused
				}
				e.witness(contract.StaticFilePath(i), []string{"internal", "n1-clipboard-diagnostic", "invoke"})
				r, runErr := callChild(ctx, contract.StaticFilePath(i), []string{"internal", "n1-clipboard-diagnostic", "invoke"}, raw, 16384)
				if !r.closed {
					e.localDirty = true
				}
				if runErr == nil && decode(r.raw, &inv, 16384) == nil && inv.Validate() == nil && inv.Binding == o {
					var cli *clipboarddiag.Fragment
					for k, f := range inv.Fragments {
						if f.Origin == "cli" {
							v := inv.Fragments[k]
							cli = &v
						}
					}
					if cli != nil {
						q := struct {
							Version   int                     `json:"version"`
							Session   string                  `json:"session"`
							Operation clipboarddiag.Operation `json:"operation"`
							CLI       clipboarddiag.Fragment  `json:"cli"`
						}{1, roleName(i), o, *cli}
						input, _ := json.Marshal(q)
						if e.executable(i) != nil {
							return nil, 1, ErrRefused
						}
						e.witness(contract.StaticFilePath(i), []string{"internal", "n1-clipboard-diagnostic", "collect"})
						col, ce := callChild(ctx, contract.StaticFilePath(i), []string{"internal", "n1-clipboard-diagnostic", "collect"}, input, 32768)
						if !col.closed {
							e.localDirty = true
						}
						if ce == nil && decode(col.raw, &collection, 32768) == nil && collection.Validate() == nil && collection.Binding == o {
						} else {
							collection = clipboarddiag.CollectionReceipt{}
						}
					}
				}
				verdict := adjudicate.Clipboard(inv, collection)
				results[i][j] = verdict
				if j == 0 {
					copyOK = copyAcknowledged(inv, o)
				}
				if i == 0 && (verdict.Outcome != "committed" || !verdict.Equal || !verdict.MetadataComplete) {
					stockOK = false
				}
				count, overlay := 0, 0
				for _, f := range collection.Fragments {
					count += len(f.Records)
				}
				if collection.Overlay != nil {
					overlay = len(collection.Overlay.Records)
				}
				// Only admitted typed synthetic/diagnostic metadata leaves memory. A raw
				// collection can reach32768 bytes and is never copied into the archive.
				fixture := inv.Synthetic
				if fixture.Validate() != nil {
					fixture = clipboarddiag.SyntheticResult{FixtureID: "n1_clipboard_control_v1", ExpectedSHA256: "9096af926f38bc69facaa4d383ba789f106a6506fadf6cedd170616b882f88e7"}
				}
				return clipboardReceipt{1, verdict, o, fixture, count, overlay}, 2, nil
			})
			if err != nil {
				return err
			}
		}
	}
	raw, _ := contract.Encode(struct {
		Version              int                              `json:"version"`
		StockComparisonValid bool                             `json:"stock_comparison_valid"`
		CandidateSkipped     bool                             `json:"candidate_skipped"`
		Results              [2][2]adjudicate.ClipboardResult `json:"results"`
	}{1, stockOK, !stockOK, results})
	return e.archive.write("clipboard-verdict.json", raw)
}

func copyAcknowledged(r clipboarddiag.InvocationReceipt, o clipboarddiag.Operation) bool {
	return r.Validate() == nil && r.Binding == o && o.Direction == "write" && r.Outcome == "committed" && r.Synthetic.Equal && r.Synthetic.Length == len("boxwarden-n1-clipboard-control\n")
}
