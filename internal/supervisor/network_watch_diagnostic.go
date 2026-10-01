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
			ArmDiagnosticWatchReceipt(context.Context, networkdiag.Arm) (networkdiag.ArmReceipt, error)
		}); ok {
			response.Receipt, err = typed.ArmDiagnosticWatchReceipt(ctx, r.Arm)
			response.OK = err == nil && ctx.Err() == nil && response.Receipt.Matches(r.Arm) && response.Receipt.Deadline.WallNS > uint64(time.Now().UnixNano())
		}
		if ctx.Err() != nil {
			return true
		}
		data, err = networkdiag.Encode(response)
	} else if action == "n1_network_observe" {
		var r networkObserveRequest
		if networkdiag.Decode(raw, &r) != nil || r.Version != 1 || r.Binding != b {
			return true
		}
		ctx, cancel, ok := networkDeadline(parent, c, accepted, r.ExpiresUnixNS)
		if !ok {
			return true
		}
		defer cancel()
		response := networkObserveResponse{Version: 1, Binding: b}
		if typed, ok := owner.(interface {
			ObserveDiagnosticLaunch(context.Context) (networkdiag.LaunchObservation, error)
		}); ok {
			response.Observation, err = typed.ObserveDiagnosticLaunch(ctx)
			response.OK = err == nil && ctx.Err() == nil && response.Observation.Valid() && networkExact(b, response.Observation.Inspection.Binding)
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
	r, e := c.ArmDiagnosticWatchReceipt(ctx, b, a)
	return r.Armed, e
}
func (c *Client) ArmDiagnosticWatchReceipt(ctx context.Context, b Binding, a networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	if !networkExact(b, a.Candidate) || !a.Valid(a.Generation, a.Nonce, a.Candidate.MAC, a.Gateway) {
		return networkdiag.ArmReceipt{}, networkdiag.ErrMetadata
	}
	var response networkArmResponse
	err := c.networkCall(ctx, b, &networkArmRequest{Version: 1, Action: "n1_network_arm", Binding: b, Arm: a}, &response)
	now, ce := networkdiag.HostClockNow()
	if err != nil || ce != nil || response.Version != 1 || response.Binding != b || !response.OK || !response.Receipt.Matches(a) || !response.Receipt.Current(now) {
		return networkdiag.ArmReceipt{}, networkdiag.ErrMetadata
	}
	return response.Receipt, nil
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

func (c *ExactController) ArmDiagnosticWatchReceipt(ctx context.Context, b Binding, a networkdiag.Arm) (networkdiag.ArmReceipt, error) {
	if c == nil {
		return networkdiag.ArmReceipt{}, networkdiag.ErrMetadata
	}
	dir, e := c.runtimeDirectory(b)
	if e != nil {
		return networkdiag.ArmReceipt{}, e
	}
	return (&Client{RuntimeDirectory: dir}).ArmDiagnosticWatchReceipt(ctx, b, a)
}
func (c *Client) ObserveDiagnosticLaunch(ctx context.Context, b Binding) (networkdiag.LaunchObservation, error) {
	var response networkObserveResponse
	e := c.networkCall(ctx, b, &networkObserveRequest{Version: 1, Action: "n1_network_observe", Binding: b}, &response)
	now, ce := networkdiag.HostClockNow()
	if e != nil || ce != nil || response.Version != 1 || response.Binding != b || !response.OK || !response.Observation.Valid() || !networkExact(b, response.Observation.Inspection.Binding) || !beforeLaunchDeadline(now, response.Observation) || ctx.Err() != nil {
		return networkdiag.LaunchObservation{}, networkdiag.ErrMetadata
	}
	return response.Observation, nil
}
func beforeLaunchDeadline(now networkdiag.ClockReading, o networkdiag.LaunchObservation) bool {
	return now.WallNS >= o.Watch.Observed.WallNS && now.ContinuousNS >= o.Watch.Observed.ContinuousNS && now.WallNS < o.Watch.Deadline.WallNS && now.ContinuousNS < o.Watch.Deadline.ContinuousNS && now.WallNS-o.Watch.Observed.WallNS <= uint64(networkControlTimeout)
}
func (r *ExactSnapshotReader) ObserveDiagnosticLaunch(ctx context.Context, b Binding) (networkdiag.LaunchObservation, error) {
	if r == nil {
		return networkdiag.LaunchObservation{}, networkdiag.ErrMetadata
	}
	dir, e := exactRuntimeDirectory(r.runtimeRoot, b)
	if e != nil {
		return networkdiag.LaunchObservation{}, e
	}
	return (&Client{RuntimeDirectory: dir}).ObserveDiagnosticLaunch(ctx, b)
}
func (c *ExactController) ObserveDiagnosticLaunch(ctx context.Context, b Binding) (networkdiag.LaunchObservation, error) {
	if c == nil {
		return networkdiag.LaunchObservation{}, networkdiag.ErrMetadata
	}
	dir, e := c.runtimeDirectory(b)
	if e != nil {
		return networkdiag.LaunchObservation{}, e
	}
	return (&Client{RuntimeDirectory: dir}).ObserveDiagnosticLaunch(ctx, b)
}
