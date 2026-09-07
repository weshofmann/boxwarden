//go:build !darwin && !linux

package supervisor

import "fmt"

func renameWithoutReplace(string, string) error {
	return fmt.Errorf("atomic no-replace generation publication is unavailable on this platform")
}
