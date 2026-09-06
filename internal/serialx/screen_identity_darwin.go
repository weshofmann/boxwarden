//go:build darwin && cgo

package serialx

/*
#cgo LDFLAGS: -lproc
#include <errno.h>
#include <libproc.h>
#include <stdint.h>
#include <string.h>
#include <sys/proc_info.h>

#define BW_PROC_PIDUNIQIDENTIFIERINFO 17
// Exact pinned-Darwin/XNU ABI for PROC_PIDUNIQIDENTIFIERINFO (flavor 17).
// libproc exposes no public C declaration for this record. Keep this layout
// and size assertion synchronized with the qualified Darwin release.
struct bw_unique_info {
	uint8_t executable_uuid[16];
	uint64_t unique_id;
	uint64_t parent_unique_id;
	uint8_t reserved[24];
};
_Static_assert(sizeof(struct bw_unique_info) == 56, "unexpected PROC_PIDUNIQIDENTIFIERINFO ABI");

static int bw_screen_identity(int pid, uint64_t *unique, int64_t *start_micros) {
	struct bw_unique_info before, after;
	struct proc_bsdinfo bsd;
	memset(&before, 0, sizeof(before));
	memset(&after, 0, sizeof(after));
	memset(&bsd, 0, sizeof(bsd));
	errno = 0;
	if (proc_pidinfo(pid, BW_PROC_PIDUNIQIDENTIFIERINFO, 0, &before, sizeof(before)) != (int)sizeof(before)) return errno ? errno : ESRCH;
	errno = 0;
	if (proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &bsd, sizeof(bsd)) != (int)sizeof(bsd)) return errno ? errno : ESRCH;
	errno = 0;
	if (proc_pidinfo(pid, BW_PROC_PIDUNIQIDENTIFIERINFO, 0, &after, sizeof(after)) != (int)sizeof(after)) return errno ? errno : ESRCH;
	if (bsd.pbi_pid != pid || before.unique_id == 0 || before.unique_id != after.unique_id) return ESRCH;
	*unique = before.unique_id;
	*start_micros = (int64_t)(bsd.pbi_start_tvsec * 1000000ULL + bsd.pbi_start_tvusec);
	return 0;
}
*/
import "C"

import (
	"context"
	"errors"
	"syscall"
	"time"
)

func observeScreenIdentity(ctx context.Context, pid int) (screenIdentity, error) {
	if err := ctx.Err(); err != nil {
		return screenIdentity{}, err
	}
	if pid <= 0 {
		return screenIdentity{}, errors.New("invalid Screen PID")
	}
	var unique C.uint64_t
	var start C.int64_t
	if code := C.bw_screen_identity(C.int(pid), &unique, &start); code != 0 {
		return screenIdentity{}, syscall.Errno(code)
	}
	if unique == 0 || start <= 0 {
		return screenIdentity{}, errors.New("incomplete Screen kernel identity")
	}
	return screenIdentity{pid: pid, unique: uint64(unique), started: time.UnixMicro(int64(start))}, nil
}
