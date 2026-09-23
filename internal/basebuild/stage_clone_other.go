//go:build !darwin || !cgo

package basebuild

import "os"

func cloneStagedFile(_ *os.File, _ string, _ os.FileMode) (bool, error) {
	return false, nil
}
