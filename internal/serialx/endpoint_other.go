//go:build !darwin && !linux

package serialx

import (
	"fmt"
	"os"
)

func openEndpointLink(string) (*os.File, error) {
	return nil, fmt.Errorf("retained endpoint symlink identity is unavailable on this platform")
}
