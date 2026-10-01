//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

/*
#cgo LDFLAGS: -lproc
#include <libproc.h>
#include <sys/proc_info.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>
struct bw_unique {uint8_t executable_uuid[16];uint64_t unique_id;uint64_t parent_id;uint8_t reserved[24];};
struct bw_identity {uint32_t pid;uint64_t birth;uint64_t unique;char path[PROC_PIDPATHINFO_MAXSIZE];};
static int bw_checked_count(int count,int error,int capacity){if(error||count<=0||count>=capacity)return -1;return count;}
 static int bw_pids(int *values,int capacity){errno=0;int count=proc_listallpids(values,capacity*sizeof(int));return bw_checked_count(count,errno,capacity);}
static int bw_identity(int pid,struct bw_identity *out){
 struct proc_bsdinfo b={0};struct bw_unique u={0};memset(out,0,sizeof(*out));errno=0;
 if(pid<=0||proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&b,sizeof(b))!=sizeof(b)||errno)return -1;
 errno=0;if(proc_pidinfo(pid,17,0,&u,sizeof(u))!=sizeof(u)||errno||!u.unique_id)return -1;
 if(b.pbi_pid!=pid||b.pbi_start_tvusec>=1000000||b.pbi_start_tvsec>(UINT64_MAX-b.pbi_start_tvusec)/1000000)return -1;
 out->pid=b.pbi_pid;out->birth=b.pbi_start_tvsec*1000000+b.pbi_start_tvusec;out->unique=u.unique_id;
 errno=0;int n=proc_pidpath(pid,out->path,sizeof(out->path));if(errno||n<=0||n>=sizeof(out->path)||!memchr(out->path,0,sizeof(out->path))||!out->path[0])return -1;return 0;
}
*/
import "C"
import (
	"context"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"unsafe"
)

type nativeSampler struct{ catalogue contract.Catalogue }

func (s nativeSampler) snapshot(ctx context.Context) ([]process, error) {
	var values [maxProcesses + 1]C.int
	n := int(C.bw_pids((*C.int)(unsafe.Pointer(&values[0])), C.int(len(values))))
	if n <= 0 || n > maxProcesses {
		return nil, ErrRefused
	}
	xs := make([]process, 0, n)
	for _, pid := range values[:n] {
		if ctx.Err() != nil {
			return nil, ErrRefused
		}
		p, e := nativeProcess(ctx, int(pid), s.catalogue)
		if e != nil {
			return nil, ErrRefused
		}
		xs = append(xs, p)
	}
	return xs, nil
}
func nativeProcess(ctx context.Context, pid int, catalogue contract.Catalogue) (process, error) {
	var before, after C.struct_bw_identity
	if C.bw_identity(C.int(pid), &before) != 0 {
		return process{}, ErrRefused
	}
	path := C.GoString(&before.path[0])
	image, e := inspectProcessImage(ctx, path, catalogue)
	if e != nil {
		return process{}, ErrRefused
	}
	if e != nil || C.bw_identity(C.int(pid), &after) != 0 || before.pid != after.pid || before.birth != after.birth || before.unique != after.unique || path != C.GoString(&after.path[0]) {
		return process{}, ErrRefused
	}
	return process{PID: pid, Birth: uint64(before.birth), Unique: uint64(before.unique), Path: path, SHA: image.sha, Device: image.device, Inode: image.inode, Kind: image.kind, QualificationSHA: image.qualification}, nil
}

func checkedNativeCount(count, code int) int {
	return int(C.bw_checked_count(C.int(count), C.int(code), C.int(maxProcesses+1)))
}
