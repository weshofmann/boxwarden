package workspacex

import (
	"context"
	"fmt"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// AcquireStorageOperation serializes a domain's workspace record and volume
// transitions. Multi-session operations acquire sorted session locks first,
// then this lock, and the golden lock last if required.
func AcquireStorageOperation(ctx context.Context, stateRoot string, domainID domain.ID) (*lock.Held, error) {
	if _, err := domain.Parse(string(domainID)); err != nil {
		return nil, err
	}
	return lock.Acquire(ctx, stateRoot, "storage-"+string(domainID))
}

// AcquireVolumeUse is held by the supervisor through backend stop/wait/reap.
// Releasing it never clears the durable Use reservation or proves backend stop.
func AcquireVolumeUse(ctx context.Context, stateRoot string, domainID domain.ID, volumeID string) (*lock.Held, error) {
	if _, err := domain.Parse(string(domainID)); err != nil {
		return nil, err
	}
	if !validUUID(volumeID) {
		return nil, fmt.Errorf("invalid volume ID")
	}
	return lock.Acquire(ctx, stateRoot, "volume-"+string(domainID)+"-"+volumeID)
}
