//go:build !darwin || !cgo

package hostidentity

import "fmt"

func checkStorage(StorageExpectation) error {
	return fmt.Errorf("workspace storage identity requires macOS with cgo")
}
