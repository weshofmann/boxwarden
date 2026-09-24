//go:build !darwin

package importx

import (
	"errors"
	"fmt"
	"os"
)

// Other platforms are test-only for this Tart-backed alpha. The staging
// parent is owner-private and cooperating import callers serialize the same
// transaction. Production Darwin uses an atomic no-replace rename.
func renameExclusive(parent *os.Root, from, to string) error {
	if _, err := parent.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("import destination changed: %v", err)
	}
	return parent.Rename(from, to)
}
