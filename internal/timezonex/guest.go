package timezonex

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/sshx"
)

// ZoneClient is intentionally limited to sshx's existing typed time-zone
// operations; guest convergence cannot acquire a generic remote execution seam.
type ZoneClient interface {
	ApplyZone(context.Context, sshx.Connection, sshx.ApplyZoneRequest) error
	ReadZone(context.Context, sshx.Connection, sshx.ReadZoneRequest) (string, error)
}

// Converge applies the trusted host zone then requires an exact typed readback.
func Converge(ctx context.Context, client ZoneClient, connection sshx.Connection, zone string) error {
	if client == nil {
		return fmt.Errorf("time-zone SSH client is required")
	}
	if !Valid(zone) {
		return fmt.Errorf("invalid host time zone")
	}
	if err := client.ApplyZone(ctx, connection, sshx.ApplyZoneRequest{Zone: zone}); err != nil {
		return fmt.Errorf("apply guest time zone: %w", err)
	}
	actual, err := client.ReadZone(ctx, connection, sshx.ReadZoneRequest{})
	if err != nil {
		return fmt.Errorf("read guest time zone: %w", err)
	}
	if !Valid(actual) || actual != zone {
		return fmt.Errorf("guest time zone %q does not match host time zone %q", actual, zone)
	}
	return nil
}
