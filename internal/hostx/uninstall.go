package hostx

import (
	"context"
	"fmt"
)

// ConsumerChecker observes both live and recorded supervisor ownership. An
// error is deliberately a refusal: uninstall may not guess that a digest tree
// is unused.
type ConsumerChecker interface {
	HasConsumer(context.Context, string) (bool, error)
}

func CheckUninstallable(ctx context.Context, digest string, consumers ConsumerChecker) error {
	if digest != SoftnetExecutableSHA256 {
		return fmt.Errorf("uninstall requires the exact manifested Softnet digest")
	}
	if consumers == nil {
		return fmt.Errorf("uninstall requires a verifiable consumer inventory")
	}
	active, err := consumers.HasConsumer(ctx, digest)
	if err != nil {
		return fmt.Errorf("refuse uninstall: cannot verify consumers: %w", err)
	}
	if active {
		return fmt.Errorf("refuse uninstall: a live or recorded supervisor uses digest %s", digest)
	}
	return nil
}
