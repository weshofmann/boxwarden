package fake

import (
	"context"

	"github.com/weshofmann/boxwarden/internal/backend"
)

type Observer struct {
	Observations map[string]backend.Observation
	Err          error
}

func (o Observer) Observe(_ context.Context, objectID string) (backend.Observation, error) {
	if o.Err != nil {
		return backend.Observation{}, o.Err
	}
	if observation, ok := o.Observations[objectID]; ok {
		return observation, nil
	}
	return backend.Observation{ObjectID: objectID, State: backend.ObjectUnknown}, nil
}
