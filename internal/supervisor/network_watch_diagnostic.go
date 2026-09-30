//go:build n1diagnostic && !n1candidate

package supervisor

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"net"
	"time"
)

func handleNetworkWatchControl(parent context.Context, c net.Conn, b Binding, owner RuntimeOwner, raw []byte, accepted time.Time, action string) bool {
	var data []byte
	var err error
	if action == "n1_network_arm" {
		var r networkArmRequest
		if networkdiag.Decode(raw, &r) != nil || r.Version != 1 || r.Binding != b || !networkExact(b, r.Arm.Candidate) {
			return true
		}
		ctx, cancel, ok := networkDeadline(parent, c, accepted, r.ExpiresUnixNS)
		if !ok {
			return true
		}
		defer cancel()
		response := networkArmResponse{Version: 1, Binding: b}
		if typed, ok := owner.(interface {
			ArmDiagnosticWatch(context.Context, networkdiag.Arm) (networkdiag.Armed, error)
		}); ok {
			response.Armed, err = typed.ArmDiagnosticWatch(ctx, r.Arm)
			response.OK = err == nil && ctx.Err() == nil && response.Armed.Matches(r.Arm)
		}
		if ctx.Err() != nil {
			return true
		}
		data, err = networkdiag.Encode(response)
	} else {
		var r networkCollectRequest
		if networkdiag.Decode(raw, &r) != nil || r.Version != 1 || r.Binding != b || !networkdiag.UUID(r.OperationID) {
			return true
		}
		ctx, cancel, ok := networkDeadline(parent, c, accepted, r.ExpiresUnixNS)
		if !ok {
			return true
		}
		defer cancel()
		response := networkCollectResponse{Version: 1, Binding: b}
		if typed, ok := owner.(interface {
			CollectDiagnosticWatch(context.Context, string) (networkdiag.Summary, error)
		}); ok {
			response.Summary, err = typed.CollectDiagnosticWatch(ctx, r.OperationID)
			response.OK = err == nil && ctx.Err() == nil && response.Summary.OperationID == r.OperationID && networkExact(b, response.Summary.Candidate) && response.Summary.ValidStandalone()
		}
		if ctx.Err() != nil {
			return true
		}
		data, err = networkdiag.Encode(response)
	}
	if err == nil {
		_ = writeFrame(c, data)
	}
	return true
}
func (c *Client) ArmDiagnosticWatch(ctx context.Context, b Binding, a networkdiag.Arm) (networkdiag.Armed, error) {
	if !networkExact(b, a.Candidate) || !a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) {
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	var response networkArmResponse
	err := c.networkCall(ctx, b, &networkArmRequest{Version: 1, Action: "n1_network_arm", Binding: b, Arm: a}, &response)
	if err != nil || response.Version != 1 || response.Binding != b || !response.OK || !response.Armed.Matches(a) {
		return networkdiag.Armed{}, networkdiag.ErrMetadata
	}
	return response.Armed, nil
}
func (c *Client) CollectDiagnosticWatch(ctx context.Context, b Binding, operation string) (networkdiag.Summary, error) {
	if !networkdiag.UUID(operation) {
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	var response networkCollectResponse
	err := c.networkCall(ctx, b, &networkCollectRequest{Version: 1, Action: "n1_network_collect", Binding: b, OperationID: operation}, &response)
	if err != nil || response.Version != 1 || response.Binding != b || !response.OK || response.Summary.OperationID != operation || !networkExact(b, response.Summary.Candidate) || !response.Summary.ValidStandalone() {
		return networkdiag.Summary{}, networkdiag.ErrMetadata
	}
	return response.Summary, nil
}
func (c *ExactController) ArmDiagnosticWatch(ctx context.Context, b Binding, a networkdiag.Arm) (networkdiag.Armed, error) {
	dir, err := c.runtimeDirectory(b)
	if err != nil {
		return networkdiag.Armed{}, err
	}
	return (&Client{RuntimeDirectory: dir}).ArmDiagnosticWatch(ctx, b, a)
}
func (c *ExactController) CollectDiagnosticWatch(ctx context.Context, b Binding, operation string) (networkdiag.Summary, error) {
	dir, err := c.runtimeDirectory(b)
	if err != nil {
		return networkdiag.Summary{}, err
	}
	return (&Client{RuntimeDirectory: dir}).CollectDiagnosticWatch(ctx, b, operation)
}
