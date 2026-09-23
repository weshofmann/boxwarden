package tart

import (
	"context"
	"errors"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func closeManagedDiskLifetimes(disks []*backend.ManagedDiskLifetime) error {
	var result error
	for _, disk := range disks {
		result = errors.Join(result, disk.Close())
	}
	return result
}

// managedDiskHandle retains every raw-file descriptor and volume-use lock
// until the exact Tart child has reaped. A canceled or unverifiable wait
// deliberately preserves them for a later recovery attempt.
type managedDiskHandle struct {
	backend.Handle
	disks []*backend.ManagedDiskLifetime
}

func (h *managedDiskHandle) Wait(ctx context.Context) error {
	err := h.Handle.Wait(ctx)
	if ctx.Err() != nil || errors.Is(err, ErrReapUnproven) {
		return err
	}
	return errors.Join(err, closeManagedDiskLifetimes(h.disks))
}
