package backend

import "context"

type ObjectState string

const (
	ObjectRunning ObjectState = "running"
	ObjectStopped ObjectState = "stopped"
	ObjectUnknown ObjectState = "unknown"
)

func (s ObjectState) Valid() bool {
	return s == ObjectRunning || s == ObjectStopped || s == ObjectUnknown
}

type Observation struct {
	ObjectID   string
	Exists     bool
	State      ObjectState
	Diagnostic string
}

type Observer interface {
	Observe(context.Context, string) (Observation, error)
}
