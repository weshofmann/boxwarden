//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package n1

/*
#cgo LDFLAGS: -lproc
#include <libproc.h>
#include <sys/proc_info.h>
#include <sys/proc.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>
struct bw_unique {uint8_t executable_uuid[16];uint64_t unique_id;uint64_t parent_id;uint8_t reserved[24];};
struct bw_identity {uint32_t pid;uint64_t birth;uint64_t unique;char path[PROC_PIDPATHINFO_MAXSIZE];};
struct bw_bsd_result {struct proc_bsdinfo info; int result; int error;};
struct bw_path_result {char path[PROC_PIDPATHINFO_MAXSIZE]; int result; int error;};
static struct bw_bsd_result bw_bsd(int pid){struct bw_bsd_result out={0};errno=0;out.result=proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&out.info,sizeof(out.info));out.error=errno;return out;}
static struct bw_path_result bw_path(int pid){struct bw_path_result out={0};errno=0;out.result=proc_pidpath(pid,out.path,sizeof(out.path));out.error=errno;return out;}
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
	"bytes"
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
	pids := make([]int, n)
	for i, p := range values[:n] {
		pids[i] = int(p)
	}
	return nativeSnapshot(ctx, pids, func(pid int) (process, error) { return nativeProcess(ctx, pid, s.catalogue) })
}

// The same classification loop is exercised with injected observations only.
func nativeSnapshot(ctx context.Context, pids []int, observe func(int) (process, error)) ([]process, error) {
	if ctx.Err() != nil || len(pids) == 0 || len(pids) > maxProcesses || observe == nil {
		return nil, ErrRefused
	}
	seen := map[int]bool{}
	zero := false
	for _, pid := range pids {
		if pid < 0 || seen[pid] {
			return nil, ErrRefused
		}
		seen[pid] = true
		if pid == 0 {
			zero = true
		}
	}
	if !zero {
		return nil, ErrRefused
	}
	xs := make([]process, 0, len(pids))
	for _, pid := range pids {
		if ctx.Err() != nil {
			return nil, ErrRefused
		}
		p, e := observe(pid)
		if e != nil || p.PID != pid || pid == 0 && !kernelProcessValid(p) {
			return nil, ErrRefused
		}
		xs = append(xs, p)
	}
	if ctx.Err() != nil {
		return nil, ErrRefused
	}
	return xs, nil
}

const nativeBSDSize = int(C.sizeof_struct_proc_bsdinfo)
const nativePathSize = int(C.PROC_PIDPATHINFO_MAXSIZE)

type nativeBSD struct {
	Selected              kernelObservation
	Seconds, Microseconds uint64
	Comm                  [C.MAXCOMLEN]byte
	Name                  [2 * C.MAXCOMLEN]byte
	Result, Code          int
}
type nativePath struct {
	Buffer       [nativePathSize]byte
	Result, Code int
}
type kernelCalls struct {
	bsd  func(int) nativeBSD
	path func(int) nativePath
}

func liveBSD(pid int) nativeBSD {
	r := C.bw_bsd(C.int(pid))
	b := r.info
	x := nativeBSD{Selected: kernelObservation{PID: uint32(b.pbi_pid), PPID: uint32(b.pbi_ppid), UID: uint32(b.pbi_uid), GID: uint32(b.pbi_gid), RUID: uint32(b.pbi_ruid), RGID: uint32(b.pbi_rgid), SUID: uint32(b.pbi_svuid), SGID: uint32(b.pbi_svgid), Status: uint32(b.pbi_status), Flags: uint32(b.pbi_flags)}, Seconds: uint64(b.pbi_start_tvsec), Microseconds: uint64(b.pbi_start_tvusec), Result: int(r.result), Code: int(r.error)}
	copy(x.Comm[:], C.GoBytes(unsafe.Pointer(&b.pbi_comm[0]), C.int(len(x.Comm))))
	copy(x.Name[:], C.GoBytes(unsafe.Pointer(&b.pbi_name[0]), C.int(len(x.Name))))
	return x
}
func livePath(pid int) nativePath {
	r := C.bw_path(C.int(pid))
	x := nativePath{Result: int(r.result), Code: int(r.error)}
	copy(x.Buffer[:], C.GoBytes(unsafe.Pointer(&r.path[0]), C.int(len(x.Buffer))))
	return x
}
func selectedKernel(b nativeBSD) (kernelObservation, error) {
	if b.Result != nativeBSDSize || b.Code != 0 || b.Selected.Status != uint32(C.SRUN) || b.Selected.Flags != uint32(C.PROC_FLAG_SYSTEM|C.PROC_FLAG_LP64) || b.Microseconds >= 1000000 || b.Seconds > (^uint64(0)-b.Microseconds)/1000000 {
		return kernelObservation{}, ErrRefused
	}
	name := func(v []byte) string {
		n := bytes.IndexByte(v, 0)
		if n < 0 {
			return ""
		}
		return string(v[:n])
	}
	k := b.Selected
	k.Comm = name(b.Comm[:])
	k.Name = name(b.Name[:])
	k.Birth = b.Seconds*1000000 + b.Microseconds
	if !k.valid() {
		return kernelObservation{}, ErrRefused
	}
	return k, nil
}
func observeKernel(ctx context.Context, c kernelCalls) (process, error) {
	if ctx.Err() != nil || c.bsd == nil || c.path == nil {
		return process{}, ErrRefused
	}
	before, e := selectedKernel(c.bsd(0))
	if e != nil || ctx.Err() != nil {
		return process{}, ErrRefused
	}
	p := c.path(0)
	if p.Result != 0 || p.Code != int(C.ESRCH) || p.Buffer != ([nativePathSize]byte{}) || ctx.Err() != nil {
		return process{}, ErrRefused
	}
	after, e := selectedKernel(c.bsd(0))
	if e != nil || before != after || ctx.Err() != nil {
		return process{}, ErrRefused
	}
	return process{PID: 0, Birth: before.Birth, Kind: "kernel", Kernel: before}, nil
}

func nativeProcess(ctx context.Context, pid int, catalogue contract.Catalogue) (process, error) {
	if pid == 0 {
		return observeKernel(ctx, kernelCalls{liveBSD, livePath})
	}
	if pid < 0 || ctx.Err() != nil {
		return process{}, ErrRefused
	}
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
