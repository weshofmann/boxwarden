//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package supervisor

import (
	"context"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/networkdiag"
	"net"
	"strings"
	"testing"
	"time"
)

type networkTestOwner struct {
	*clipboardTestOwner
	inspection   networkdiag.Inspection
	networkCalls int
}

func (o *networkTestOwner) InspectDiagnosticNetwork(context.Context) (networkdiag.Inspection, error) {
	o.networkCalls++
	return o.inspection, nil
}
func TestDiagnosticNetworkPrivateExactDispatch(t *testing.T) {
	for _, kind := range []string{"valid", "duplicate", "extra", "binding", "expiry"} {
		t.Run(kind, func(t *testing.T) {
			b := minimalRequest(t).Binding
			b.Domain = "n1qualification"
			b.SessionID = "00000000-0000-4000-8000-000000000001"
			b.Generation = "00000000-0000-4000-8000-000000000011"
			o := &networkTestOwner{clipboardTestOwner: &clipboardTestOwner{binding: b, ready: true}, inspection: networkdiag.Inspection{Binding: networkdiag.Binding{Domain: b.Domain, SessionID: b.SessionID, Generation: b.Generation, BackendKind: b.BackendKind, BackendObject: b.BackendObject, Address: [4]uint8{192, 168, 64, 2}, MAC: [6]uint8{2, 0, 0, 0, 0, 2}}, PinFingerprint: "SHA256:" + strings.Repeat("A", 43), ConfigSHA256: strings.Repeat("a", 64), ObservedUnixNS: uint64(time.Now().UnixNano())}}
			req := networkInspectRequest{1, "n1_network_inspect", b, uint64(time.Now().Add(time.Second).UnixNano())}
			if kind == "binding" {
				req.Binding.Generation = "00000000-0000-4000-8000-000000000099"
			}
			if kind == "expiry" {
				req.ExpiresUnixNS = 1
			}
			raw, _ := json.Marshal(req)
			if kind == "duplicate" {
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1))
			}
			if kind == "extra" {
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"root":"/arbitrary"`, 1))
			}
			server, client := net.Pipe()
			done := make(chan struct{})
			go func() { handleControl(t.Context(), server, b, o, func() error { return nil }); close(done) }()
			client.SetDeadline(time.Now().Add(2 * time.Second))
			if err := writeFrame(client, raw); err != nil {
				t.Fatal(err)
			}
			out, err := readBounded(client)
			client.Close()
			<-done
			if kind == "valid" {
				var got networkInspectResponse
				if err != nil || networkdiag.Decode(out, &got) != nil || !got.OK || got.Inspection != o.inspection || o.networkCalls != 1 {
					t.Fatalf("private dispatch=%s %v calls=%d", out, err, o.networkCalls)
				}
			} else if err == nil || o.networkCalls != 0 {
				t.Fatalf("malformed dispatch=%s %v calls=%d", out, err, o.networkCalls)
			}
		})
	}
}
