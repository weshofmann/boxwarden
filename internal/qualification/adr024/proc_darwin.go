//go:build darwin && cgo

package adr024

/*
#cgo LDFLAGS: -lproc
#include <errno.h>
#include <libproc.h>
#include <stdint.h>
#include <string.h>
#include <sys/proc_info.h>

struct bw_proc_info {
	int32_t pid;
	int32_t ppid;
	int32_t pgid;
	uint64_t unique_id;
	uint64_t parent_unique_id;
	int32_t euid;
	int32_t egid;
	int32_t ruid;
	int32_t rgid;
	int32_t svuid;
	int32_t svgid;
	int64_t start_time_micros;
	char path[PROC_PIDPATHINFO_MAXSIZE];
};

// PROC_PIDUNIQIDENTIFIERINFO is intentionally a private libproc flavor. This
// qualification-only observer uses only the stable process and parent IDs at
// the front of the fixed-size result. XNU admits this flavor without its
// same-effective-UID check; any host policy denial remains fatal. These IDs
// close PID-reuse races that the public short BSD structure cannot close alone.
#define BW_PROC_PIDUNIQIDENTIFIERINFO 17
struct bw_proc_unique_info {
	uint8_t executable_uuid[16];
	uint64_t unique_id;
	uint64_t parent_unique_id;
	uint8_t reserved[24];
};

static int bw_short_info(int pid, struct bw_proc_info *out) {
	struct proc_bsdshortinfo source;
	memset(&source, 0, sizeof(source));
	errno = 0;
	int result = proc_pidinfo(pid, PROC_PIDT_SHORTBSDINFO, 0, &source, sizeof(source));
	if (result != (int)sizeof(source)) {
		return errno != 0 ? errno : ESRCH;
	}
	memset(out, 0, sizeof(*out));
	out->pid = (int32_t)source.pbsi_pid;
	out->ppid = (int32_t)source.pbsi_ppid;
	out->pgid = (int32_t)source.pbsi_pgid;
	out->euid = (int32_t)source.pbsi_uid;
	out->egid = (int32_t)source.pbsi_gid;
	out->ruid = (int32_t)source.pbsi_ruid;
	out->rgid = (int32_t)source.pbsi_rgid;
	out->svuid = (int32_t)source.pbsi_svuid;
	out->svgid = (int32_t)source.pbsi_svgid;
	return 0;
}

static int bw_full_bsd_info(int pid, struct bw_proc_info *out) {
	struct proc_bsdinfo source;
	memset(&source, 0, sizeof(source));
	errno = 0;
	int result = proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &source, sizeof(source));
	if (result != (int)sizeof(source)) {
		return errno != 0 ? errno : ESRCH;
	}
	memset(out, 0, sizeof(*out));
	out->pid = (int32_t)source.pbi_pid;
	out->ppid = (int32_t)source.pbi_ppid;
	out->pgid = (int32_t)source.pbi_pgid;
	out->euid = (int32_t)source.pbi_uid;
	out->egid = (int32_t)source.pbi_gid;
	out->ruid = (int32_t)source.pbi_ruid;
	out->rgid = (int32_t)source.pbi_rgid;
	out->svuid = (int32_t)source.pbi_svuid;
	out->svgid = (int32_t)source.pbi_svgid;
	out->start_time_micros = (int64_t)(source.pbi_start_tvsec * 1000000ULL + source.pbi_start_tvusec);
	return 0;
}

static int bw_unique_info(int pid, struct bw_proc_info *out) {
	struct bw_proc_unique_info source;
	memset(&source, 0, sizeof(source));
	errno = 0;
	int result = proc_pidinfo(pid, BW_PROC_PIDUNIQIDENTIFIERINFO, 0, &source, sizeof(source));
	if (result != (int)sizeof(source)) {
		return errno != 0 ? errno : ESRCH;
	}
	out->unique_id = source.unique_id;
	out->parent_unique_id = source.parent_unique_id;
	return 0;
}

static int bw_path_info(int pid, struct bw_proc_info *out) {
	errno = 0;
	if (proc_pidpath(pid, out->path, sizeof(out->path)) <= 0) {
		return errno != 0 ? errno : ESRCH;
	}
	return 0;
}

static int bw_child_pids(int ppid, int *pids, int capacity) {
	errno = 0;
	int count = proc_listchildpids((pid_t)ppid, pids, capacity * (int)sizeof(int));
	if (count == 0 && errno != 0) {
		return -errno;
	}
	return count;
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

const maxDirectChildren = 3

type systemSampler struct{}

func newSystemSampler() (Sampler, error) { return systemSampler{}, nil }

func (systemSampler) Sample(ctx context.Context, tartPID int) (Sample, error) {
	if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	tart, err := observedProcessInfo(tartPID, true)
	if err != nil {
		return Sample{}, fmt.Errorf("query Tart process: %w", err)
	}
	childPIDs, err := directChildPIDs(tartPID)
	if err != nil {
		return Sample{}, fmt.Errorf("enumerate direct child processes: %w", err)
	}
	children := make([]ProcessInfo, 0, len(childPIDs))
	for _, childPID := range childPIDs {
		if err := ctx.Err(); err != nil {
			return Sample{}, err
		}
		child, err := observedProcessInfo(childPID, false)
		if err != nil {
			return Sample{}, fmt.Errorf("query direct child process %d: %w", childPID, err)
		}
		children = append(children, child)
	}
	return Sample{Tart: tart, Children: children}, nil
}

func directChildPIDs(ppid int) ([]int, error) {
	var values [maxDirectChildren]C.int
	count := int(C.bw_child_pids(C.int(ppid), (*C.int)(unsafe.Pointer(&values[0])), C.int(len(values))))
	if count < 0 {
		return nil, syscall.Errno(-count)
	}
	if count > len(values) {
		return nil, errors.New("direct child result exceeded fixed capacity")
	}
	result := make([]int, 0, count)
	for index := 0; index < count; index++ {
		if values[index] <= 0 {
			return nil, errors.New("direct child result contained an invalid PID")
		}
		result = append(result, int(values[index]))
	}
	return result, nil
}

func shortProcessInfo(pid int) (ProcessInfo, error) {
	var value C.struct_bw_proc_info
	if code := C.bw_short_info(C.int(pid), &value); code != 0 {
		return ProcessInfo{}, syscall.Errno(code)
	}
	return goProcessInfo(value), nil
}

func fullBSDProcessInfo(pid int) (ProcessInfo, error) {
	var value C.struct_bw_proc_info
	if code := C.bw_full_bsd_info(C.int(pid), &value); code != 0 {
		return ProcessInfo{}, syscall.Errno(code)
	}
	return goProcessInfo(value), nil
}

func uniqueProcessIdentity(pid int) (uint64, uint64, error) {
	var value C.struct_bw_proc_info
	if code := C.bw_unique_info(C.int(pid), &value); code != 0 {
		return 0, 0, syscall.Errno(code)
	}
	if value.unique_id == 0 || value.parent_unique_id == 0 {
		return 0, 0, errors.New("process unique identity is incomplete")
	}
	return uint64(value.unique_id), uint64(value.parent_unique_id), nil
}

func processPath(pid int) (string, error) {
	var value C.struct_bw_proc_info
	if code := C.bw_path_info(C.int(pid), &value); code != 0 {
		return "", syscall.Errno(code)
	}
	path := C.GoString(&value.path[0])
	if path == "" {
		return "", errors.New("process executable path is empty")
	}
	return path, nil
}

func observedProcessInfo(pid int, requireStartTime bool) (ProcessInfo, error) {
	uniqueBefore, parentBefore, err := uniqueProcessIdentity(pid)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("query process unique identity before sample: %w", err)
	}
	result, err := shortProcessInfo(pid)
	if err != nil {
		return ProcessInfo{}, err
	}
	result.UniqueID = uniqueBefore
	result.ParentUniqueID = parentBefore
	result.Executable, err = processPath(pid)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("query process executable path: %w", err)
	}
	if requireStartTime || result.Credentials.EffectiveUID == os.Geteuid() {
		full, fullErr := fullBSDProcessInfo(pid)
		if fullErr != nil {
			return ProcessInfo{}, fmt.Errorf("query full BSD process identity: %w", fullErr)
		}
		if full.PID != result.PID || full.PPID != result.PPID || full.PGID != result.PGID || full.Credentials != result.Credentials {
			return ProcessInfo{}, errors.New("process identity changed within sample")
		}
		result.StartTimeUnixMicros = full.StartTimeUnixMicros
	}
	uniqueAfter, parentAfter, err := uniqueProcessIdentity(pid)
	if err != nil {
		return ProcessInfo{}, fmt.Errorf("query process unique identity after sample: %w", err)
	}
	if uniqueBefore != uniqueAfter || parentBefore != parentAfter {
		return ProcessInfo{}, errors.New("process unique identity changed within sample")
	}
	return result, nil
}

func goProcessInfo(value C.struct_bw_proc_info) ProcessInfo {
	result := ProcessInfo{
		PID: int(value.pid), PPID: int(value.ppid), PGID: int(value.pgid),
		UniqueID: uint64(value.unique_id), ParentUniqueID: uint64(value.parent_unique_id),
		Credentials: Credentials{
			EffectiveUID: int(value.euid), EffectiveGID: int(value.egid),
			RealUID: int(value.ruid), RealGID: int(value.rgid),
			SavedUID: int(value.svuid), SavedGID: int(value.svgid),
		},
	}
	result.StartTimeUnixMicros = int64(value.start_time_micros)
	return result
}
