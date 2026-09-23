//go:build !darwin && !(linux && (amd64 || arm64))

package exportx

import "fmt"

func renameNoReplace(int, string, string) error {
	return fmt.Errorf("atomic no-replace export publication unavailable on this platform")
}
