package timezonex

import (
	"context"
	"testing"

	"github.com/weshofmann/boxwarden/internal/sshx"
)

func TestConvergeAppliesThenReadsTheTypedSSHZoneInterface(t *testing.T) {
	client := &zoneClientFake{read: "America/Chihuahua"}
	connection := sshx.Connection{}
	if err := Converge(context.Background(), client, connection, "America/Chihuahua"); err != nil {
		t.Fatalf("Converge() error = %v", err)
	}
	if client.apply.Zone != "America/Chihuahua" || client.applyCalls != 1 || client.readCalls != 1 {
		t.Fatalf("typed calls = %#v, apply=%d read=%d", client, client.applyCalls, client.readCalls)
	}
}

func TestConvergeRejectsMismatchedReadback(t *testing.T) {
	client := &zoneClientFake{read: "America/Denver"}
	if err := Converge(context.Background(), client, sshx.Connection{}, "America/Chihuahua"); err == nil {
		t.Fatal("Converge() error = nil, want readback mismatch")
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
