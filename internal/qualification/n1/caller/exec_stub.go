//go:build !darwin

package caller

import (
	"os"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/fixed"
)

func replaceImage(string, []string, []string, *os.File) error { return fixed.ErrRefused }
