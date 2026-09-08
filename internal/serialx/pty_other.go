//go:build !darwin || !cgo

package serialx

import (
	"fmt"
	"os"
)

func allocatePTY() (*os.File, *os.File, error) {
	return nil, nil, fmt.Errorf("Darwin PTY allocation is unavailable on this platform")
}
