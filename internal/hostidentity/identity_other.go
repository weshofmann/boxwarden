//go:build !darwin || !cgo

package hostidentity

import (
	"fmt"
	"os"
)

func observe(*os.File) (Identity, error) {
	return Identity{}, fmt.Errorf("persistent APFS identity requires macOS with cgo")
}
