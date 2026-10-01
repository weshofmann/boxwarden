//go:build n1clipboarddiagnostic && !n1candidate

package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/clock"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/sshx"
)

type Request struct {
	Version   int               `json:"version"`
	Window    contract.Window   `json:"window"`
	Identity  Identity          `json:"identity"`
	Phase     sshx.N1GuestPhase `json:"phase,omitempty"`
	Peer      *sshx.N1Peer      `json:"peer,omitempty"`
	Watch     *sshx.N1PeerWatch `json:"watch,omitempty"`
	Arm       *networkdiag.Arm  `json:"arm,omitempty"`
	Operation string            `json:"operation,omitempty"`
}

func parseRequest(raw []byte, command string) (Request, error) {
	var r Request
	if len(raw) < 1 || len(raw) > contract.MaxReceiptBytes || raw[len(raw)-1] != '\n' {
		return r, ErrRefused
	}
	d := json.NewDecoder(bytes.NewReader(raw[:len(raw)-1]))
	d.DisallowUnknownFields()
	if d.Decode(&r) != nil {
		return r, ErrRefused
	}
	canonical, e := bounded(r, contract.MaxReceiptBytes)
	if e != nil || !bytes.Equal(raw, canonical) || r.Version != 1 || !r.Window.Valid() {
		return r, ErrRefused
	}
	base := r
	base.Phase = ""
	base.Peer = nil
	base.Watch = nil
	base.Arm = nil
	base.Operation = ""
	switch command {
	case "discover":
		if !createdIdentity(r.Identity) || r.Identity.Generation != "" {
			return r, ErrRefused
		}
	case "create":
		if r.Identity != (Identity{}) {
			return r, ErrRefused
		}
	case "inspect", "inspect-running", "inspect-stopped", "inspect-deleted", "stage", "controls", "positive":
		if !r.Identity.valid() {
			return r, ErrRefused
		}
	case "inspect-guest":
		if !r.Identity.valid() || (r.Phase != sshx.N1GuestInitial && r.Phase != sshx.N1GuestFinal) {
			return r, ErrRefused
		}
		base.Phase = r.Phase
	case "observe":
		if !r.Identity.valid() || r.Watch == nil {
			return r, ErrRefused
		}
		base.Watch = r.Watch
	case "connect":
		if !r.Identity.valid() || r.Peer == nil || role != "candidate" {
			return r, ErrRefused
		}
		base.Peer = r.Peer
	case "arm":
		if !r.Identity.valid() || r.Arm == nil || role != "candidate" {
			return r, ErrRefused
		}
		base.Arm = r.Arm
	case "collect":
		if !r.Identity.valid() || !contract.UUID(r.Operation) || role != "candidate" {
			return r, ErrRefused
		}
		base.Operation = r.Operation
	default:
		return r, ErrRefused
	}
	if a, _ := json.Marshal(base); !bytes.Equal(a, bytes.TrimSuffix(canonical, []byte{'\n'})) {
		return r, ErrRefused
	}
	return r, nil
}
func Run(observer backend.Observer, creator backend.Creator) error {
	if len(os.Args) != 2 {
		return ErrRefused
	}
	raw, e := io.ReadAll(io.LimitReader(os.Stdin, contract.MaxReceiptBytes+1))
	if e != nil {
		return ErrRefused
	}
	request, e := parseRequest(raw, os.Args[1])
	if e != nil {
		return ErrRefused
	}
	w, e := Compose(observer, creator)
	if e != nil || w.static.LockSHA != request.Window.LockSHA {
		return ErrRefused
	}
	g := clock.Guard{Window: request.Window}
	check := func() error {
		r, e := clock.Now()
		if e != nil || g.Check(r) != nil {
			return ErrRefused
		}
		return nil
	}
	if check() != nil {
		return ErrRefused
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(0, int64(request.Window.ExpiresUnixNS)))
	defer cancel()
	var result any
	switch os.Args[1] {
	case "discover":
		result, e = w.Discover(ctx, request.Identity)
	case "inspect-running":
		result, e = w.InspectRunning(ctx, request.Identity)
	case "inspect-stopped":
		result, e = w.InspectStopped(ctx, request.Identity)
	case "inspect-deleted":
		result, e = w.InspectDeleted(ctx, request.Identity)
	case "create":
		result, e = w.Create(ctx)
	case "inspect":
		result, e = w.Inspect(ctx, request.Identity)
	case "stage":
		result, e = w.Stage(ctx, request.Identity)
	case "inspect-guest":
		result, e = w.InspectGuest(ctx, request.Identity, request.Phase)
	case "controls":
		result, e = w.Controls(ctx, request.Identity)
	case "positive":
		e = w.Positive(ctx, request.Identity)
		if e == nil {
			result = struct {
				Version int  `json:"version"`
				OK      bool `json:"ok"`
			}{1, true}
		}
	case "observe":
		result, e = w.ObservePeer(ctx, request.Identity, *request.Watch, func(r sshx.N1PeerReady) error {
			if check() != nil {
				return ErrRefused
			}
			return writeOutput(r, 8192)
		})
	case "connect":
		result, e = w.Connect(ctx, request.Identity, *request.Peer)
	case "arm":
		result, e = w.Arm(ctx, request.Identity, *request.Arm)
	case "collect":
		result, e = w.Collect(ctx, request.Identity, request.Operation)
	}
	// A completed connect fact survives a later transport error. It is emitted as
	// bounded typed data with a nonzero exit; it never converts that error to success.
	if e != nil {
		if c, ok := result.(sshx.N1ConnectResult); ok && c.Version == 1 {
			writeOutput(c, 4096)
		}
		return ErrRefused
	}
	if check() != nil {
		return ErrRefused
	}
	limit := contract.MaxReceiptBytes
	if os.Args[1] == "observe" {
		limit = 8192
	}
	return writeOutput(result, limit)
}
func writeOutput(v any, limit int) error {
	raw, e := bounded(v, limit)
	if e != nil {
		return e
	}
	n, e := os.Stdout.Write(raw)
	if e != nil || n != len(raw) {
		return ErrRefused
	}
	return nil
}
