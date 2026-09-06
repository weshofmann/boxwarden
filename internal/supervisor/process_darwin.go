//go:build darwin && cgo

package supervisor

/*
#cgo LDFLAGS: -lproc
#include <errno.h>
#include <libproc.h>
#include <stdint.h>
#include <string.h>
#include <sys/proc_info.h>
#define BW_PROC_PIDUNIQIDENTIFIERINFO 17
struct bw_unique_info { uint8_t executable_uuid[16]; uint64_t unique_id; uint64_t parent_unique_id; uint8_t reserved[24]; };
_Static_assert(sizeof(struct bw_unique_info) == 56, "unexpected PROC_PIDUNIQIDENTIFIERINFO ABI");
static int bw_supervisor_identity(int pid, uint64_t *unique, int64_t *start_micros) {
 struct bw_unique_info before, after; struct proc_bsdinfo bsd; memset(&before,0,sizeof(before)); memset(&after,0,sizeof(after)); memset(&bsd,0,sizeof(bsd));
 errno=0; if(proc_pidinfo(pid,BW_PROC_PIDUNIQIDENTIFIERINFO,0,&before,sizeof(before))!=(int)sizeof(before)) return errno?errno:ESRCH;
 errno=0; if(proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&bsd,sizeof(bsd))!=(int)sizeof(bsd)) return errno?errno:ESRCH;
 errno=0; if(proc_pidinfo(pid,BW_PROC_PIDUNIQIDENTIFIERINFO,0,&after,sizeof(after))!=(int)sizeof(after)) return errno?errno:ESRCH;
 if(bsd.pbi_pid!=pid||before.unique_id==0||before.unique_id!=after.unique_id) return ESRCH;
 *unique=before.unique_id; *start_micros=(int64_t)(bsd.pbi_start_tvsec*1000000ULL+bsd.pbi_start_tvusec); return 0;
}
*/
import "C"
import (
	"context"
	"fmt"
	"syscall"
	"time"
)

type systemInspector struct{}

func (systemInspector) Supported() bool { return true }
func (systemInspector) Observe(ctx context.Context, pid int) (ProcessIdentity, error) {
	if err := ctx.Err(); err != nil {
		return ProcessIdentity{}, err
	}
	if pid <= 0 {
		return ProcessIdentity{}, fmt.Errorf("invalid PID")
	}
	var unique C.uint64_t
	var started C.int64_t
	if code := C.bw_supervisor_identity(C.int(pid), &unique, &started); code != 0 {
		return ProcessIdentity{}, syscall.Errno(code)
	}
	identity := ProcessIdentity{PID: pid, Unique: uint64(unique), StartedAt: time.UnixMicro(int64(started))}
	if !identity.valid() {
		return ProcessIdentity{}, fmt.Errorf("incomplete Darwin process identity")
	}
	return identity, nil
}
