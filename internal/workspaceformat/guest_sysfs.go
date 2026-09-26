package workspaceformat

import (
	"os"
	"path/filepath"
)

// linuxHasPartitions looks for the kernel's partition marker only under
// directory children of a block device. Ordinary sysfs attributes such as
// size and dev are files; appending /partition to them returns ENOTDIR.
func linuxHasPartitions(sysPath string) (bool, error) {
	entries, err := os.ReadDir(sysPath)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		child := filepath.Join(sysPath, entry.Name())
		info, err := os.Stat(child)
		if err != nil {
			return false, err
		}
		if !info.IsDir() {
			continue
		}
		_, err = os.Stat(filepath.Join(child, "partition"))
		if err == nil {
			return true, nil
		}
		if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}
