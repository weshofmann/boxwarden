//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package closeout

/*
#include <sys/attr.h>
#include <unistd.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>

struct bw_mount_uuid_result { uint32_t length; uint8_t uuid[16]; } __attribute__((packed));

static int bw_mount_uuid(int fd, uint8_t uuid[16]) {
    struct attrlist attrs = {0};
    attrs.bitmapcount = ATTR_BIT_MAP_COUNT;
    attrs.volattr = ATTR_VOL_INFO | ATTR_VOL_UUID;
    struct bw_mount_uuid_result result = {0};
    if (fgetattrlist(fd, &attrs, &result, sizeof(result), 0) != 0) return errno;
    if (result.length != sizeof(result)) return EPROTO;
    memcpy(uuid, result.uuid, 16);
    return 0;
}
*/
import "C"

import (
	"encoding/hex"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

type mountedVolume struct {
	UUID string
	FSID syscall.Fsid
}

func requireUniqueMountedUUID(expected string, active syscall.Fsid, mounts []mountedVolume) error {
	found := false
	for _, mount := range mounts {
		if mount.UUID != expected {
			continue
		}
		if mount.FSID != active {
			return fmt.Errorf("ambiguous APFS volume UUID %s on distinct mounted filesystems", expected)
		}
		found = true
	}
	if !found {
		return fmt.Errorf("pinned APFS volume is absent from mounted inventory")
	}
	return nil
}

func mountedAPFSVolumes() ([]mountedVolume, error) {
	const mntNowait = 2
	count, err := syscall.Getfsstat(nil, mntNowait)
	if err != nil || count < 1 || count > 4096 {
		return nil, fmt.Errorf("enumerate mounted filesystems: count=%d: %w", count, err)
	}
	entries := make([]syscall.Statfs_t, count+16)
	n, err := syscall.Getfsstat(entries, mntNowait)
	if err != nil || n > len(entries) {
		return nil, fmt.Errorf("mounted filesystem inventory changed: count=%d: %w", n, err)
	}
	mounts := make([]mountedVolume, 0, n)
	seen := make(map[syscall.Fsid]bool)
	for _, entry := range entries[:n] {
		if int8String(entry.Fstypename[:]) != "apfs" || seen[entry.Fsid] {
			continue
		}
		seen[entry.Fsid] = true
		path := int8String(entry.Mntonname[:])
		file, openErr := os.OpenFile(path, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW, 0)
		if openErr != nil {
			return nil, fmt.Errorf("open mounted APFS volume %q: %w", path, openErr)
		}
		var opened syscall.Statfs_t
		if err := syscall.Fstatfs(int(file.Fd()), &opened); err != nil {
			file.Close()
			return nil, fmt.Errorf("inspect mounted APFS volume %q: %w", path, err)
		}
		if !sameMountedFilesystem(entry, opened) {
			file.Close()
			return nil, fmt.Errorf("mounted APFS path %q changed during inventory", path)
		}
		var uuid [16]byte
		code := C.bw_mount_uuid(C.int(file.Fd()), (*C.uint8_t)(unsafe.Pointer(&uuid[0])))
		closeErr := file.Close()
		if code != 0 {
			return nil, fmt.Errorf("read mounted APFS volume %q UUID: %w", path, syscall.Errno(code))
		}
		if closeErr != nil {
			return nil, closeErr
		}
		raw := hex.EncodeToString(uuid[:])
		mounts = append(mounts, mountedVolume{UUID: raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], FSID: entry.Fsid})
	}
	return mounts, nil
}

func sameMountedFilesystem(enumerated, opened syscall.Statfs_t) bool {
	return enumerated.Fsid == opened.Fsid && int8String(enumerated.Fstypename[:]) == "apfs" && int8String(opened.Fstypename[:]) == "apfs"
}

func int8String(raw []int8) string {
	text := make([]byte, 0, len(raw))
	for _, b := range raw {
		if b == 0 {
			break
		}
		text = append(text, byte(b))
	}
	return string(text)
}
