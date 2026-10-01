//go:build darwin && n1clipboarddiagnostic && !n1candidate

package worker

import "syscall"

func nativeMeta(s *syscall.Stat_t) (int64, uint32) {
	return s.Ctimespec.Sec*1e9 + s.Ctimespec.Nsec, s.Flags
}
