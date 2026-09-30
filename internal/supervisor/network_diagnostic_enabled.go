//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package supervisor

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"net"
	"path/filepath"
	"time"
)

type networkInspectRequest struct {
	Version       uint8   `json:"version"`
	Action        string  `json:"action"`
	Binding       Binding `json:"binding"`
	ExpiresUnixNS uint64  `json:"expires_unix_ns"`
}
type networkInspectResponse struct {
	Version    uint8                  `json:"version"`
	Binding    Binding                `json:"binding"`
	OK         bool                   `json:"ok"`
	Inspection networkdiag.Inspection `json:"inspection"`
}

const networkControlTimeout = 90 * time.Second

func networkExact(b Binding, n networkdiag.Binding) bool {
	return b.Domain == n.Domain && b.SessionID == n.SessionID && b.Generation == n.Generation && b.BackendKind == n.BackendKind && b.BackendObject == n.BackendObject
}
func networkDeadline(parent context.Context, c net.Conn, accepted time.Time, expiry uint64) (context.Context, context.CancelFunc, bool) {
	if expiry > uint64(1<<63-1) {
		return nil, nil, false
	}
	deadline := time.Unix(0, int64(expiry))
	if !deadline.After(time.Now()) || deadline.After(accepted.Add(networkControlTimeout)) {
		return nil, nil, false
	}
	if d, ok := parent.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if !deadline.After(time.Now()) || c.SetDeadline(deadline) != nil {
		return nil, nil, false
	}
	ctx, cancel := context.WithDeadline(parent, deadline)
	return ctx, cancel, true
}
func handleNetworkDiagnosticControl(parent context.Context, c net.Conn, b Binding, owner RuntimeOwner, raw []byte, accepted time.Time) bool {
	var kind struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(raw, &kind) != nil {
		return false
	}
	if kind.Action == "n1_network_arm" || kind.Action == "n1_network_collect" {
		return handleNetworkWatchControl(parent, c, b, owner, raw, accepted, kind.Action)
	}
	if kind.Action != "n1_network_inspect" {
		return false
	}
	var r networkInspectRequest
	if networkdiag.Decode(raw, &r) != nil || r.Version != 1 || r.Binding != b {
		return true
	}
	ctx, cancel, ok := networkDeadline(parent, c, accepted, r.ExpiresUnixNS)
	if !ok {
		return true
	}
	defer cancel()
	response := networkInspectResponse{Version: 1, Binding: b}
	if inspector, ok := owner.(interface {
		InspectDiagnosticNetwork(context.Context) (networkdiag.Inspection, error)
	}); ok {
		result, err := inspector.InspectDiagnosticNetwork(ctx)
		if err == nil && ctx.Err() == nil && result.Valid() && networkExact(b, result.Binding) {
			response.OK = true
			response.Inspection = result
		}
	}
	data, err := networkdiag.Encode(response)
	if err == nil && ctx.Err() == nil {
		_ = writeFrame(c, data)
	}
	return true
}
func (c *Client) networkCall(ctx context.Context, b Binding, request any, response any) error {
	if c == nil || !b.valid() || ctx.Err() != nil {
		return networkdiag.ErrMetadata
	}
	launch, err := readLaunchRequest(filepath.Join(c.RuntimeDirectory, requestName))
	if err != nil || launch.Binding != b || validateGenerationEntry(c.RuntimeDirectory, socketName) != nil {
		return networkdiag.ErrMetadata
	}
	state, err := classifyExactGeneration(launch)
	if err != nil || state != exactGenerationLive {
		return networkdiag.ErrMetadata
	}
	operation, cancel := context.WithTimeout(ctx, networkControlTimeout)
	defer cancel()
	deadline, _ := operation.Deadline()
	switch r := request.(type) {
	case *networkInspectRequest:
		r.ExpiresUnixNS = uint64(deadline.UnixNano())
	case *networkArmRequest:
		r.ExpiresUnixNS = uint64(deadline.UnixNano())
	case *networkCollectRequest:
		r.ExpiresUnixNS = uint64(deadline.UnixNano())
	default:
		return networkdiag.ErrMetadata
	}
	data, err := networkdiag.Encode(request)
	if err != nil {
		return err
	}
	conn, err := dialControl(operation, filepath.Join(c.RuntimeDirectory, socketName))
	if err != nil {
		return networkdiag.ErrMetadata
	}
	defer conn.Close()
	if conn.SetDeadline(deadline) != nil {
		return networkdiag.ErrMetadata
	}
	stop := context.AfterFunc(operation, func() { conn.SetDeadline(time.Now()) })
	defer stop()
	if writeFrame(conn, data) != nil {
		return networkdiag.ErrMetadata
	}
	raw, err := networkdiag.ReadFrame(conn)
	if err != nil || operation.Err() != nil || networkdiag.Decode(raw, response) != nil {
		return networkdiag.ErrMetadata
	}
	return nil
}
func (c *Client) InspectDiagnosticNetwork(ctx context.Context, b Binding) (networkdiag.Inspection, error) {
	var response networkInspectResponse
	err := c.networkCall(ctx, b, &networkInspectRequest{Version: 1, Action: "n1_network_inspect", Binding: b}, &response)
	if err != nil || response.Version != 1 || response.Binding != b || !response.OK || !response.Inspection.Valid() || !networkExact(b, response.Inspection.Binding) {
		return networkdiag.Inspection{}, networkdiag.ErrMetadata
	}
	now := uint64(time.Now().UnixNano())
	if response.Inspection.ObservedUnixNS > now || now-response.Inspection.ObservedUnixNS > uint64(networkControlTimeout) {
		return networkdiag.Inspection{}, networkdiag.ErrMetadata
	}
	return response.Inspection, nil
}
func (r *ExactSnapshotReader) InspectDiagnosticNetwork(ctx context.Context, b Binding) (networkdiag.Inspection, error) {
	if r == nil {
		return networkdiag.Inspection{}, networkdiag.ErrMetadata
	}
	dir, err := exactRuntimeDirectory(r.runtimeRoot, b)
	if err != nil {
		return networkdiag.Inspection{}, err
	}
	return (&Client{RuntimeDirectory: dir}).InspectDiagnosticNetwork(ctx, b)
}
func (c *ExactController) InspectDiagnosticNetwork(ctx context.Context, b Binding) (networkdiag.Inspection, error) {
	if c == nil {
		return networkdiag.Inspection{}, networkdiag.ErrMetadata
	}
	dir, err := c.runtimeDirectory(b)
	if err != nil {
		return networkdiag.Inspection{}, err
	}
	return (&Client{RuntimeDirectory: dir}).InspectDiagnosticNetwork(ctx, b)
}
