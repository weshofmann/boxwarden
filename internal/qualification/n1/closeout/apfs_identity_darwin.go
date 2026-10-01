//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package closeout

/*
#include <sys/attr.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <unistd.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>

struct bw_uuid_result { uint32_t length; uint8_t uuid[16]; } __attribute__((packed));
struct bw_cap_result { uint32_t length; vol_capabilities_attr_t caps; } __attribute__((packed));
struct bw_id_result { uint32_t length; uint64_t id; } __attribute__((packed));

static int bw_apfs_identity(int fd, uint8_t uuid[16], uint64_t *fileid) {
    struct statfs fs;
    if (fstatfs(fd, &fs) != 0) return errno;
    if (strcmp(fs.f_fstypename, "apfs") != 0) return ENOTSUP;

    struct attrlist attrs = {0};
    attrs.bitmapcount = ATTR_BIT_MAP_COUNT;
    attrs.volattr = ATTR_VOL_INFO | ATTR_VOL_UUID;
    struct bw_uuid_result volume = {0};
    if (fgetattrlist(fd, &attrs, &volume, sizeof(volume), 0) != 0) return errno;
    if (volume.length != sizeof(volume)) return EPROTO;

    attrs.volattr = ATTR_VOL_INFO | ATTR_VOL_CAPABILITIES;
    struct bw_cap_result capabilities = {0};
    if (fgetattrlist(fd, &attrs, &capabilities, sizeof(capabilities), 0) != 0) return errno;
    if (capabilities.length != sizeof(capabilities)) return EPROTO;
    uint32_t required = VOL_CAP_FMT_PERSISTENTOBJECTIDS | VOL_CAP_FMT_64BIT_OBJECT_IDS;
    if ((capabilities.caps.valid[VOL_CAPABILITIES_FORMAT] & required) != required ||
        (capabilities.caps.capabilities[VOL_CAPABILITIES_FORMAT] & required) != required) return ENOTSUP;

    attrs.volattr = 0;
    attrs.commonattr = ATTR_CMN_FILEID;
    struct bw_id_result ordinary = {0};
    if (fgetattrlist(fd, &attrs, &ordinary, sizeof(ordinary), 0) != 0) return errno;
    if (ordinary.length != sizeof(ordinary)) return EPROTO;

    attrs.commonattr = ATTR_CMN_OBJPERMANENTID;
    struct bw_id_result permanent = {0};
    if (fgetattrlist(fd, &attrs, &permanent, sizeof(permanent), 0) != 0) return errno;
    if (permanent.length != sizeof(permanent)) return EPROTO;

    struct stat st;
    if (fstat(fd, &st) != 0) return errno;
    if (ordinary.id == 0 || permanent.id == 0 ||
        ordinary.id != permanent.id || permanent.id != (uint64_t)st.st_ino) return EPROTO;
    uint8_t zero[16] = {0};
    if (memcmp(volume.uuid, zero, sizeof(zero)) == 0) return EPROTO;
    memcpy(uuid, volume.uuid, 16);
    *fileid = permanent.id;
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

func observeAPFS(file *os.File) (apfsIdentity, error) {
	if file == nil {
		return apfsIdentity{}, fmt.Errorf("pinned host file is required")
	}
	var uuid [16]byte
	var fileID C.uint64_t
	code := C.bw_apfs_identity(C.int(file.Fd()), (*C.uint8_t)(unsafe.Pointer(&uuid[0])), &fileID)
	if code != 0 {
		return apfsIdentity{}, fmt.Errorf("read pinned APFS identity: %w", syscall.Errno(code))
	}
	raw := hex.EncodeToString(uuid[:])
	identity := apfsIdentity{VolumeUUID: raw[:8] + "-" + raw[8:12] + "-" + raw[12:16] + "-" + raw[16:20] + "-" + raw[20:], FileID: uint64(fileID)}
	var filesystem syscall.Statfs_t
	if err := syscall.Fstatfs(int(file.Fd()), &filesystem); err != nil {
		return apfsIdentity{}, fmt.Errorf("read pinned filesystem: %w", err)
	}
	mounts, err := mountedAPFSVolumes()
	if err != nil {
		return apfsIdentity{}, err
	}
	if err := requireUniqueMountedUUID(identity.VolumeUUID, filesystem.Fsid, mounts); err != nil {
		return apfsIdentity{}, err
	}
	return identity, nil
}
