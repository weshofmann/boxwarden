//go:build n1diagnostic && !n1candidate && darwin && cgo

package networkdiag

/*
#include <libproc.h>
#include <sys/proc_info.h>
#include <sys/proc.h>
#include <stdint.h>
#include <errno.h>
struct bw_diag_unique {uint8_t executable_uuid[16];uint64_t unique_id;uint64_t parent_id;uint8_t reserved[24];};
static int bw_diag_self(int pid,uint64_t *birth,uint64_t *unique){struct proc_bsdinfo b={0},a={0};struct bw_diag_unique u={0},v={0};errno=0;
if(pid<=0||proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&b,sizeof(b))!=sizeof(b)||errno||b.pbi_pid!=pid||b.pbi_status==SZOMB||b.pbi_start_tvusec>=1000000||b.pbi_start_tvsec>(UINT64_MAX-b.pbi_start_tvusec)/1000000)return 1;
errno=0;if(proc_pidinfo(pid,17,0,&u,sizeof(u))!=sizeof(u)||errno||!u.unique_id)return 1;
errno=0;if(proc_pidinfo(pid,PROC_PIDTBSDINFO,0,&a,sizeof(a))!=sizeof(a)||errno||a.pbi_pid!=b.pbi_pid||a.pbi_status==SZOMB||a.pbi_start_tvsec!=b.pbi_start_tvsec||a.pbi_start_tvusec!=b.pbi_start_tvusec)return 1;
errno=0;if(proc_pidinfo(pid,17,0,&v,sizeof(v))!=sizeof(v)||errno||u.unique_id!=v.unique_id)return 1;
*birth=b.pbi_start_tvsec*1000000+b.pbi_start_tvusec;*unique=u.unique_id;return !*birth||!*unique;}
*/
import "C"
import "os"

func CurrentOwnerProcess() (ProcessCorrelation, error) {
	pid := os.Getpid()
	var b, u C.uint64_t
	if pid <= 0 || C.bw_diag_self(C.int(pid), &b, &u) != 0 {
		return ProcessCorrelation{}, ErrMetadata
	}
	return ProcessCorrelation{uint32(pid), uint64(b), uint64(u)}, nil
}
