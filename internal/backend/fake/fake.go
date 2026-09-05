package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/weshofmann/boxwarden/internal/backend"
)

// CloneCall records one requested clone operation.
type CloneCall struct {
	SourceID string
	TargetID string
}

// CloneFault injects a failure at the fake's clone boundary.
type CloneFault func(context.Context, CloneCall) error

// RandomizeMACFault injects a failure at the fake's MAC mutation boundary.
type RandomizeMACFault func(context.Context, string) error

// StartCall records one backend-neutral start request.
type StartCall struct{ Request backend.StartRequest }

// Backend is a concurrency-safe, stateful in-memory backend for lifecycle
// tests. It models object collisions and mutation outcomes without emulating a
// particular hypervisor.
type Backend struct {
	mu sync.Mutex

	observations          map[string]backend.Observation
	cloneCalls            []CloneCall
	randomizeMACCalls     []string
	startCalls            []StartCall
	addressCalls          []string
	deleteCalls           []string
	addresses             map[string]string
	cloneError            error
	clonePostError        error
	randomizeMACError     error
	randomizeMACPostError error
	startError            error
	addressError          error
	deleteError           error
	cloneFault            CloneFault
	randomizeMACFault     RandomizeMACFault
}

// New constructs a fake from deterministic initial observations.
func New(observations ...backend.Observation) *Backend {
	b := &Backend{observations: make(map[string]backend.Observation, len(observations)), addresses: make(map[string]string)}
	for _, observation := range observations {
		b.observations[observation.ObjectID] = observation
	}
	return b
}

// Start transitions exactly one stopped object to running and returns the
// handle that owns only that fake start's stop/wait lifecycle.
func (b *Backend) Start(ctx context.Context, request backend.StartRequest) (backend.Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := backend.ValidateStartRequest(request); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.startCalls = append(b.startCalls, StartCall{Request: request})
	startError := b.startError
	b.mu.Unlock()
	if startError != nil {
		return nil, startError
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	observation, found := b.observations[request.ObjectID]
	if !found || !observation.Exists || observation.State != backend.ObjectStopped {
		return nil, fmt.Errorf("cannot start object %q unless it exists and is stopped", request.ObjectID)
	}
	observation.State = backend.ObjectRunning
	b.observations[request.ObjectID] = observation
	return &ownedHandle{backend: b, objectID: request.ObjectID, done: make(chan struct{})}, nil
}

// Resolve returns the configured current address on each call without caching
// it in session state.
func (b *Backend) Resolve(ctx context.Context, objectID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := backend.ValidateObjectID(objectID); err != nil {
		return "", err
	}
	b.mu.Lock()
	b.addressCalls = append(b.addressCalls, objectID)
	addressError := b.addressError
	b.mu.Unlock()
	if addressError != nil {
		return "", addressError
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	address, found := b.addresses[objectID]
	if !found {
		return "", fmt.Errorf("no current address for object %q", objectID)
	}
	return address, nil
}

// Delete removes exactly one existing fake object after recording its object
// identifier. It does not alter clone behavior or accept broad selectors.
func (b *Backend) Delete(ctx context.Context, objectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := backend.ValidateObjectID(objectID); err != nil {
		return err
	}
	b.mu.Lock()
	b.deleteCalls = append(b.deleteCalls, objectID)
	deleteError := b.deleteError
	b.mu.Unlock()
	if deleteError != nil {
		return deleteError
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	observation, found := b.observations[objectID]
	if !found || !observation.Exists {
		return fmt.Errorf("cannot delete missing object %q", objectID)
	}
	delete(b.observations, objectID)
	delete(b.addresses, objectID)
	return nil
}

type ownedHandle struct {
	backend  *Backend
	objectID string
	done     chan struct{}
	once     sync.Once
	err      error
}

func (h *ownedHandle) Stop(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h == nil || h.backend == nil {
		return fmt.Errorf("owned fake backend handle is unavailable")
	}
	h.once.Do(func() {
		h.backend.mu.Lock()
		defer h.backend.mu.Unlock()
		observation, found := h.backend.observations[h.objectID]
		if !found || !observation.Exists || observation.State != backend.ObjectRunning {
			h.err = fmt.Errorf("cannot stop unowned object %q", h.objectID)
			close(h.done)
			return
		}
		observation.State = backend.ObjectStopped
		h.backend.observations[h.objectID] = observation
		close(h.done)
	})
	return h.err
}

func (h *ownedHandle) Wait(ctx context.Context) error {
	if h == nil {
		return fmt.Errorf("owned fake backend handle is unavailable")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-h.done:
		return h.err
	}
}

func (b *Backend) Observe(ctx context.Context, objectID string) (backend.Observation, error) {
	if err := ctx.Err(); err != nil {
		return backend.Observation{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return backend.Observation{}, err
	}
	if observation, ok := b.observations[objectID]; ok {
		return observation, nil
	}
	return backend.Observation{ObjectID: objectID, State: backend.ObjectUnknown}, nil
}

func (b *Backend) Clone(ctx context.Context, sourceID, targetID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	call := CloneCall{SourceID: sourceID, TargetID: targetID}
	b.mu.Lock()
	b.cloneCalls = append(b.cloneCalls, call)
	cloneError := b.cloneError
	cloneFault := b.cloneFault
	b.mu.Unlock()
	if cloneError != nil {
		return cloneError
	}
	if cloneFault != nil {
		if err := cloneFault(ctx, call); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	source, ok := b.observations[sourceID]
	if !ok || !source.Exists {
		return fmt.Errorf("clone source %q does not exist", sourceID)
	}
	if target, ok := b.observations[targetID]; ok && target.Exists {
		return fmt.Errorf("clone target %q already exists", targetID)
	}
	source.ObjectID = targetID
	source.Exists = true
	b.observations[targetID] = source
	return b.clonePostError
}

func (b *Backend) RandomizeMAC(ctx context.Context, objectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	b.randomizeMACCalls = append(b.randomizeMACCalls, objectID)
	randomizeMACError := b.randomizeMACError
	randomizeMACFault := b.randomizeMACFault
	b.mu.Unlock()
	if randomizeMACError != nil {
		return randomizeMACError
	}
	if randomizeMACFault != nil {
		if err := randomizeMACFault(ctx, objectID); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if observation, ok := b.observations[objectID]; !ok || !observation.Exists {
		return fmt.Errorf("cannot randomize MAC for missing object %q", objectID)
	}
	return b.randomizeMACPostError
}

// SetCloneError configures a deterministic failure for every clone attempt.
func (b *Backend) SetCloneError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cloneError = err
}

// SetClonePostEffectError configures a failure returned after a clone creates
// its target, modeling an uncertain caller-visible result.
func (b *Backend) SetClonePostEffectError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.clonePostError = err
}

// SetRandomizeMACError configures a deterministic failure for every MAC
// randomization attempt.
func (b *Backend) SetRandomizeMACError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.randomizeMACError = err
}

// SetRandomizeMACPostEffectError configures a failure returned after MAC
// randomization completes, modeling an uncertain caller-visible result.
func (b *Backend) SetRandomizeMACPostEffectError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.randomizeMACPostError = err
}

// SetStartError configures a deterministic pre-effect start failure.
func (b *Backend) SetStartError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.startError = err
}

// SetAddressError configures a deterministic address-resolution failure.
func (b *Backend) SetAddressError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.addressError = err
}

// SetDeleteError configures a deterministic pre-effect deletion failure.
func (b *Backend) SetDeleteError(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleteError = err
}

// SetCloneFault configures a per-call clone fault hook.
func (b *Backend) SetCloneFault(fault CloneFault) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cloneFault = fault
}

// SetRandomizeMACFault configures a per-call MAC randomization fault hook.
func (b *Backend) SetRandomizeMACFault(fault RandomizeMACFault) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.randomizeMACFault = fault
}

// CloneCalls returns a copy of recorded clone requests in call order.
func (b *Backend) CloneCalls() []CloneCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]CloneCall(nil), b.cloneCalls...)
}

// RandomizeMACCalls returns a copy of MAC mutation requests in call order.
func (b *Backend) RandomizeMACCalls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.randomizeMACCalls...)
}

// StartCalls returns a copy of recorded start requests in call order.
func (b *Backend) StartCalls() []StartCall {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]StartCall(nil), b.startCalls...)
}

// AddressCalls returns a copy of fresh address lookups in call order.
func (b *Backend) AddressCalls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.addressCalls...)
}

// DeleteCalls returns a copy of exact deletion requests in call order.
func (b *Backend) DeleteCalls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.deleteCalls...)
}

// SetAddress replaces one non-durable fake management address.
func (b *Backend) SetAddress(objectID, address string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.addresses == nil {
		b.addresses = make(map[string]string)
	}
	b.addresses[objectID] = address
}

// SetObservation replaces the fake's observation for one object.
func (b *Backend) SetObservation(observation backend.Observation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.observations == nil {
		b.observations = make(map[string]backend.Observation)
	}
	b.observations[observation.ObjectID] = observation
}

// DeleteObservation makes an object absent from the fake's next observation.
func (b *Backend) DeleteObservation(objectID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.observations, objectID)
}

var _ backend.Observer = (*Backend)(nil)
var _ backend.Creator = (*Backend)(nil)
var _ backend.Starter = (*Backend)(nil)
var _ backend.AddressResolver = (*Backend)(nil)
var _ backend.Deleter = (*Backend)(nil)

type Observer struct {
	Observations map[string]backend.Observation
	Err          error
}

func (o Observer) Observe(ctx context.Context, objectID string) (backend.Observation, error) {
	if err := ctx.Err(); err != nil {
		return backend.Observation{}, err
	}
	if o.Err != nil {
		return backend.Observation{}, o.Err
	}
	if observation, ok := o.Observations[objectID]; ok {
		return observation, nil
	}
	return backend.Observation{ObjectID: objectID, State: backend.ObjectUnknown}, nil
}
