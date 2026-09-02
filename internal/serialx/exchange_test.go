package serialx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

func TestExchangeWritesFixedCommandThenCanonicalRequestAndReturnsMatchedPayload(t *testing.T) {
	responsePayload := json.RawMessage(`{"host_key":"ssh-ed25519 AAAA","status":"ok"}`)
	result := exchangeResultEnvelope{
		Nonce:       "nonce-1",
		Generation:  "generation-1",
		Association: Association{Domain: "work", Session: "session-1", Backend: "tart-object-1"},
		Payload:     responsePayload,
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	frame := "BOXWARDEN-BEGIN nonce-1 session-1\nBOXWARDEN-END nonce-1 session-1 " + base64.StdEncoding.EncodeToString(encodedResult) + "\n"

	tart := &recordingWriter{afterSecondWrite: func(b *Broker) { _ = b.TartOutput([]byte(frame)) }}
	broker := NewBroker(BrokerConfig{Tart: tart, Generation: "generation-1"})
	tart.broker = broker
	got, err := broker.Exchange(context.Background(), ExchangeRequest{
		Nonce:       "nonce-1",
		Generation:  "generation-1",
		Association: Association{Domain: "work", Session: "session-1", Backend: "tart-object-1"},
		Payload:     json.RawMessage(`{"ca":"fingerprint","principal":"boxwarden"}`),
	})
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if !bytes.Equal(got, responsePayload) {
		t.Fatalf("Exchange() payload = %s, want %s", got, responsePayload)
	}
	if got, want := tart.writes(), []string{
		"/usr/bin/sudo -n -- /usr/local/libexec/boxwarden-guest-bootstrap serial-bootstrap\n",
		`{"association":{"backend":"tart-object-1","domain":"work","session":"session-1"},"generation":"generation-1","nonce":"nonce-1","payload":{"ca":"fingerprint","principal":"boxwarden"}}` + "\n",
	}; !equalStrings(got, want) {
		t.Fatalf("serial writes = %#v, want %#v", got, want)
	}
	if broker.Poisoned() {
		t.Fatal("broker poisoned after one exact matched frame")
	}
}

type recordingWriter struct {
	mu               sync.Mutex
	values           []string
	broker           *Broker
	afterSecondWrite func(*Broker)
}

func (w *recordingWriter) Write(input []byte) (int, error) {
	w.mu.Lock()
	w.values = append(w.values, string(input))
	n := len(w.values)
	w.mu.Unlock()
	if n == 2 && w.afterSecondWrite != nil {
		w.afterSecondWrite(w.broker)
	}
	return len(input), nil
}

func (w *recordingWriter) writes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.values...)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
