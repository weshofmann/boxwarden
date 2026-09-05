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

// Backend is a concurrency-safe, stateful in-memory backend for lifecycle
// tests. It models object collisions and mutation outcomes without emulating a
// particular hypervisor.
type Backend struct {
	mu sync.Mutex

	observations          map[string]backend.Observation
	cloneCalls            []CloneCall
	randomizeMACCalls     []string
	cloneError            error
	clonePostError        error
	randomizeMACError     error
	randomizeMACPostError error
	cloneFault            CloneFault
	randomizeMACFault     RandomizeMACFault
}

// New constructs a fake from deterministic initial observations.
func New(observations ...backend.Observation) *Backend {
	b := &Backend{observations: make(map[string]backend.Observation, len(observations))}
	for _, observation := range observations {
		b.observations[observation.ObjectID] = observation
	}
	return b
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
