//go:build linux && !arm64 && !amd64

package guestproto

import "fmt"

// Unsupported Linux architectures fail closed rather than replacing an active
// trust tree through a check-then-rename fallback.
func renameWithoutReplacement(source, destination string) error {
	return fmt.Errorf("atomic no-replace rename is unavailable on this Linux architecture")
}
