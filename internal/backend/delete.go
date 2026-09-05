package backend

import "context"

// Deleter removes one exact backend object. It intentionally accepts only the
// object identity already bound to the session by common lifecycle code.
type Deleter interface {
	Delete(context.Context, string) error
}
