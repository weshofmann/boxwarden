//go:build linux

package hostx

import (
	"context"
)

func RunRootHostInstall(context.Context, []byte) ([]byte, error) {
	return nil, ErrUnsupportedPlatform
}
