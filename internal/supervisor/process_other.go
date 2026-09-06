//go:build !darwin || !cgo

package supervisor

import (
	"context"
	"fmt"
)

type systemInspector struct{}

func (systemInspector) Supported() bool { return false }
func (systemInspector) Observe(context.Context, int) (ProcessIdentity, error) {
	return ProcessIdentity{}, fmt.Errorf("Darwin process-start/unique identity evidence is unsupported")
}
