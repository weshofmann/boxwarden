package timezonex

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/sshx"
)

// Production break: treating successful zone application as READY without an
// exact readback would hide a guest refusal or divergent resulting zone.
func TestConvergeRequiresExactTypedZoneReadback(t *testing.T) {
	client := &zoneClientFake{read: "America/Denver"}
	if err := Converge(context.Background(), client, sshx.Connection{}, "America/Denver"); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if client.apply.Zone != "America/Denver" || client.applyCalls != 1 || client.readCalls != 1 {
		t.Fatalf("typed zone effects = %#v, want one apply/read of America/Denver", client)
	}
	client.read = "America/Chicago"
	if err := Converge(context.Background(), client, sshx.Connection{}, "America/Denver"); err == nil {
		t.Fatal("Converge() error = nil, want mismatched readback rejection")
	}
}

type zoneClientFake struct {
	apply                 sshx.ApplyZoneRequest
	read                  string
	applyCalls, readCalls int
}

func (c *zoneClientFake) ApplyZone(_ context.Context, _ sshx.Connection, request sshx.ApplyZoneRequest) error {
	c.applyCalls++
	c.apply = request
	return nil
}
func (c *zoneClientFake) ReadZone(context.Context, sshx.Connection, sshx.ReadZoneRequest) (string, error) {
	c.readCalls++
	return c.read, nil
}
