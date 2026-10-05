//go:build darwin && cgo

package hostidentity

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/privateacl"
)

func directFirstRunPath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("location must be a clean absolute path")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return fmt.Errorf("location unavailable or has symlink ancestry: %v", err)
	}
	return nil
}

func observeFirstRunLocation(dataLocation, configAncestor string) (LocationObservation, error) {
	var result LocationObservation
	if err := directFirstRunPath(dataLocation); err != nil {
		return result, err
	}
	if err := directFirstRunPath(configAncestor); err != nil {
		return result, err
	}
	data, _, err := openPrivateEnrollmentDirectory(dataLocation)
	if err != nil {
		return result, err
	}
	defer data.Close()
	outputInfo, err := os.Lstat(configAncestor)
	if err != nil {
		return result, err
	}
	stat, ok := outputInfo.Sys().(*syscall.Stat_t)
	if !ok || !outputInfo.IsDir() || int(stat.Uid) != os.Getuid() || outputInfo.Mode().Perm()&0022 != 0 {
		return result, fmt.Errorf("configuration ancestor must be operator-owned and not writable by others")
	}
	if err := privateacl.CheckSafeAncestor(configAncestor, outputInfo, privateacl.OSInspector{}); err != nil {
		return result, err
	}
	output, err := os.OpenFile(configAncestor, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return result, err
	}
	defer output.Close()
	opened, err := output.Stat()
	if err != nil || !os.SameFile(outputInfo, opened) {
		return result, fmt.Errorf("configuration ancestor changed: %v", err)
	}
	var dataFS, outputFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(data.Fd()), &dataFS); err != nil {
		return result, err
	}
	if err := syscall.Fstatfs(int(output.Fd()), &outputFS); err != nil {
		return result, err
	}
	if dataFS.Fsid == outputFS.Fsid {
		return result, fmt.Errorf("select a data location on a different mounted filesystem from configuration storage")
	}
	if int8String(outputFS.Mntonname[:]) != "/System/Volumes/Data" {
		return result, fmt.Errorf("configuration must remain on the host Data filesystem")
	}
	dataID, err := Observe(data)
	if err != nil {
		return result, err
	}
	outputID, err := Observe(output)
	if err != nil {
		return result, err
	}
	const mountReadOnly = 1 // Darwin MNT_RDONLY.
	if dataFS.Flags&mountReadOnly != 0 {
		return result, fmt.Errorf("selected data filesystem is read-only")
	}
	mountPoint := int8String(dataFS.Mntonname[:])
	mount, _, err := openPrivateEnrollmentMount(mountPoint)
	if err != nil {
		return result, err
	}
	defer mount.Close()
	var mountFS syscall.Statfs_t
	if err := syscall.Fstatfs(int(mount.Fd()), &mountFS); err != nil || mountFS.Fsid != dataFS.Fsid {
		return result, fmt.Errorf("selected data mount changed: %v", err)
	}
	if err := privateacl.CheckAncestorChain(filepath.Dir(dataLocation)); err != nil {
		return result, err
	}
	if err := privateacl.CheckAncestorChain(configAncestor); err != nil {
		return result, err
	}
	// Count only; never follow, inspect, or adopt entries beneath the selection.
	count := 0
	for {
		names, err := data.Readdirnames(256)
		count += len(names)
		if count > 100000 {
			return result, fmt.Errorf("selected location exceeds entry count limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return result, err
		}
	}
	return LocationObservation{MountPoint: mountPoint, VolumeUUID: dataID.VolumeUUID, FileID: dataID.FileID, OutputVolumeUUID: outputID.VolumeUUID, OutputFileID: outputID.FileID, AvailableBytes: dataFS.Bavail * uint64(dataFS.Bsize), CapacityBytes: dataFS.Blocks * uint64(dataFS.Bsize), OutputAvailableBytes: outputFS.Bavail * uint64(outputFS.Bsize), OutputCapacityBytes: outputFS.Blocks * uint64(outputFS.Bsize), ExistingEntries: count}, nil
}

func mountedFirstRunLocations(configAncestor string) ([]string, error) {
	const nowait = 2
	count, err := syscall.Getfsstat(nil, nowait)
	if err != nil || count < 1 || count > 4096 {
		return nil, fmt.Errorf("enumerate mounted locations: %v", err)
	}
	entries := make([]syscall.Statfs_t, count+16)
	n, err := syscall.Getfsstat(entries, nowait)
	if err != nil || n > len(entries) {
		return nil, fmt.Errorf("mounted inventory changed: %v", err)
	}
	result := []string{}
	for _, entry := range entries[:n] {
		path := int8String(entry.Mntonname[:])
		if int8String(entry.Fstypename[:]) != "apfs" || !strings.HasPrefix(path, "/Volumes/") {
			continue
		}
		if _, err := observeFirstRunLocation(path, configAncestor); err == nil {
			result = append(result, path)
		}
	}
	sort.Strings(result)
	return result, nil
}
