package tart

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/execx"
)

const (
	addressWaitSeconds  = 60
	addressCommandLimit = 70 * time.Second
	maxAddressBytes     = 128
)

// AddressResolver performs one fresh DHCP-backed Tart lookup. It intentionally
// stores no cache because an address is not backend identity.
type AddressResolver struct {
	runner     execx.Runner
	executable string
}

func NewAddressResolver(runner execx.Runner, executable string) AddressResolver {
	return AddressResolver{runner: runner, executable: executable}
}

func (r AddressResolver) Resolve(ctx context.Context, objectID string) (string, error) {
	if err := backend.ValidateObjectID(objectID); err != nil {
		return "", fmt.Errorf("resolve Tart address: %w", err)
	}
	if r.runner == nil || !canonicalAbsolutePath(r.executable) {
		return "", fmt.Errorf("resolve Tart address: qualified runner and executable are required")
	}
	commandContext, cancel := context.WithTimeout(ctx, addressCommandLimit)
	defer cancel()
	result, err := r.runner.Run(commandContext, execx.Command{Path: r.executable, Args: []string{"ip", "--resolver=dhcp", "--wait=60", objectID}})
	if err != nil {
		return "", fmt.Errorf("resolve Tart address: %w", err)
	}
	if result.Truncated || len(result.Stdout) > maxAddressBytes || len(result.Stderr) > maxAddressBytes {
		return "", fmt.Errorf("resolve Tart address: output exceeded bound")
	}
	address, err := parseAddress(result.Stdout)
	if err != nil {
		return "", fmt.Errorf("resolve Tart address: %w", err)
	}
	return address, nil
}

func parseAddress(output string) (string, error) {
	address := strings.TrimSuffix(output, "\n")
	if strings.HasSuffix(address, "\r") {
		address = strings.TrimSuffix(address, "\r")
	}
	if address == "" || strings.ContainsAny(address, "\r\n\t ") {
		return "", fmt.Errorf("expected exactly one literal IP address")
	}
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.IsValid() {
		return "", fmt.Errorf("expected exactly one literal IP address")
	}
	return parsed.String(), nil
}

var _ backend.AddressResolver = AddressResolver{}
