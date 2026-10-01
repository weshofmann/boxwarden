//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/weshofmann/boxwarden/internal/sshx"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

type controlsRunner struct {
	t       *testing.T
	binding sshx.N1GuestBinding
	calls   int
}

func (r *controlsRunner) Run(_ context.Context, command sshx.Command) (sshx.Result, error) {
	r.calls++
	var received struct {
		Binding sshx.N1GuestBinding `json:"binding"`
	}
	expected, e := json.Marshal(struct {
		Binding sshx.N1GuestBinding `json:"binding"`
	}{r.binding})
	if e != nil || command.Path != "/usr/bin/ssh" || json.Unmarshal(command.Stdin, &received) != nil || received.Binding != r.binding || string(command.Stdin) != string(expected) {
		r.t.Fatal("actual typed controls dispatch lost fixed binding")
	}
	raw, e := json.Marshal(sshx.N1ControlsResult{Version: 1, Binding: r.binding, Code: "controls", DNS: "ok", HTTPS: "ok"})
	if e != nil {
		r.t.Fatal(e)
	}
	return sshx.Result{Stdout: string(raw)}, nil
}

type controlsDriftReader struct{ *inspectReader }

func (r controlsDriftReader) Snapshot(ctx context.Context, b supervisor.Binding) (supervisor.Snapshot, error) {
	v, e := r.inspectReader.Snapshot(ctx, b)
	v.ProbeOK = false
	return v, e
}

func TestWorkerControlsAfterARMKeepsExactRuntimeAdmission(t *testing.T) {
	for _, phase := range []string{"armed", "complete"} {
		t.Run(phase, func(t *testing.T) {
			w, id, r, _ := inspectFixture(t)
			if _, e := w.Inspect(context.Background(), id); e != nil {
				t.Fatal("initial unarmed positive control", e)
			}
			r.launch.Watch.Phase = phase
			r.snapshots = 0
			r.launches = 0
			if _, e := w.Inspect(context.Background(), id); e == nil || r.launches == 0 {
				t.Fatal("public Inspect lost initial/final unarmed constraint")
			}
			r.snapshots = 0
			r.launches = 0
			transport := &controlsRunner{t: t, binding: guestBinding(id)}
			w.guest = sshx.NewClient(transport)
			got, e := w.Controls(context.Background(), id)
			if e != nil || got.Binding != guestBinding(id) || got.DNS != "ok" || got.HTTPS != "ok" || transport.calls != 1 {
				t.Fatal("post-ARM controls refused before exact typed dispatch", got, e, transport.calls)
			}
			if r.snapshots != 2 || r.launches != 0 {
				t.Fatal("controls did not bracket READY or demanded unarmed HELLO", r.snapshots, r.launches)
			}
		})
	}
}
func TestWorkerControlsAfterARMRefusesRuntimeDriftBeforeDispatch(t *testing.T) {
	for _, kind := range []string{"ready", "network", "pin", "certificate"} {
		t.Run(kind, func(t *testing.T) {
			w, id, r, _ := inspectFixture(t)
			if _, e := w.Inspect(context.Background(), id); e != nil {
				t.Fatal("initial full admission positive control", e)
			}
			r.launch.Watch.Phase = "armed"
			r.snapshots = 0
			r.launches = 0
			transport := &controlsRunner{t: t, binding: guestBinding(id)}
			w.guest = sshx.NewClient(transport)
			switch kind {
			case "ready":
				w.reader = controlsDriftReader{r}
			case "network":
				r.n.Binding.Generation = "55555555-5555-4555-8555-555555555555"
			case "pin":
				r.n.PinFingerprint = "SHA256:foreign"
			case "certificate":
				fixtureWrite(t, w.domain.StateRoot+"/runtime/n1qualification/"+id.Session+"/"+id.Generation+"/client-cert.pub", []byte("malformed certificate\n"), 0644)
			}
			if _, e := w.Controls(context.Background(), id); e == nil || transport.calls != 0 || r.launches != 0 {
				t.Fatal("post-ARM runtime drift reached guest controls", kind, e, transport.calls, r.launches)
			}
		})
	}
}
