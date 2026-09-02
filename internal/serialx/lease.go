package serialx

import (
	"context"
	"sync"
)

// Lease is the sole authority to leave console or automation mode. It is safe
// to close more than once, which lets reader EOF and owner cleanup race without
// reopening or replaying any data.
type Lease interface {
	Close() error
}

type lease struct {
	broker *Broker
	id     uint64
	state  State
	once   sync.Once
}

func (b *Broker) AcquireConsole(ctx context.Context) (Lease, error) {
	return b.acquire(ctx, StateConsole)
}

func (b *Broker) acquire(ctx context.Context, state State) (Lease, error) {
	for {
		b.mu.Lock()
		if b.state == StateFailed {
			b.mu.Unlock()
			return nil, ErrPoisoned
		}
		if b.state == StateIdle {
			b.leaseID++
			b.activeLeaseID = b.leaseID
			b.state = state
			b.notifyLocked()
			lease := &lease{broker: b, id: b.leaseID, state: state}
			b.mu.Unlock()
			return lease, nil
		}
		changed := b.changed
		b.mu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (b *Broker) release(id uint64, state State) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == state && b.activeLeaseID == id {
		b.state = StateIdle
		b.activeLeaseID = 0
		b.notifyLocked()
	}
}

func (l *lease) Close() error {
	l.once.Do(func() { l.broker.release(l.id, l.state) })
	return nil
}

// ConsoleEOF is called by the injected operator-master reader. EOF only
// releases a console lease; it never closes the retained Screen child or the
// guest serial endpoint.
func (b *Broker) ConsoleEOF() {
	b.mu.Lock()
	if b.state != StateConsole {
		b.mu.Unlock()
		return
	}
	id := b.activeLeaseID
	b.mu.Unlock()
	b.release(id, StateConsole)
}
