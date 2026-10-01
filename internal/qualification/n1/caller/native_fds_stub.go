//go:build darwin && !cgo

package caller

import "github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"

// A Go directory reader hides an fdopendir duplicate and ignores closedir's
// result. Without the checked native reader, descriptor containment is unknown.
func nativeDescriptorNames(int) ([]uintptr, uintptr, error) {
	return nil, 0, fixed.ErrRefused
}
