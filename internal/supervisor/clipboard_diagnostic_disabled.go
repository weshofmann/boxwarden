//go:build !n1clipboarddiagnostic || n1candidate

package supervisor

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"net"
	"time"
)

func clipboardStage(context.Context, string, string)                          {}
func clipboardControlLoss(context.Context)                                    {}
func clipboardDiagnosticOutcome(context.Context, clipboardx.Outcome)          {}
func clipboardDiagnosticScope(ctx context.Context, _ Binding) context.Context { return ctx }
func handleClipboardDiagnosticControl(context.Context, net.Conn, Binding, RuntimeOwner, []byte, time.Time) bool {
	return false
}
func clipboardBeginRequest(_ context.Context, b Binding, action string, expiry time.Time) ([]byte, error) {
	return json.Marshal(controlRequest{Version: 1, Binding: b, Action: action, ExpiresAt: expiry.UTC()})
}
