package serialx

import (
	"bytes"
	"context"
	"testing"
)

func TestBrokerDiscardsOperatorInputOutsideConsole(t *testing.T) {
	var tart bytes.Buffer
	broker := NewBroker(BrokerConfig{Tart: &tart})

	broker.OperatorInput([]byte("danger"))

	if got, want := broker.InputDiscarded(), uint64(len("danger")); got != want {
		t.Fatalf("InputDiscarded() = %d, want %d", got, want)
	}
	if got := tart.String(); got != "" {
		t.Fatalf("Tart input = %q, want discarded input never forwarded", got)
	}
}

func TestConsoleLeaseForwardsInputAndEOFReturnsIdle(t *testing.T) {
	var tart bytes.Buffer
	broker := NewBroker(BrokerConfig{Tart: &tart})
	lease, err := broker.AcquireConsole(context.Background())
	if err != nil {
		t.Fatalf("AcquireConsole() error = %v", err)
	}
	if got := broker.State(); got != StateConsole {
		t.Fatalf("State() = %q, want console", got)
	}

	broker.OperatorInput([]byte("ls\\n"))
	broker.ConsoleEOF()

	if got, want := tart.String(), "ls\\n"; got != want {
		t.Fatalf("Tart input = %q, want %q", got, want)
	}
	if got := broker.State(); got != StateIdle {
		t.Fatalf("State() after EOF = %q, want idle", got)
	}
	broker.OperatorInput([]byte("never\\n"))
	if got, want := tart.String(), "ls\\n"; got != want {
		t.Fatalf("Tart input after released console = %q, want %q", got, want)
	}
	if err := lease.Close(); err != nil {
		t.Fatalf("Close() after EOF error = %v", err)
	}
}
