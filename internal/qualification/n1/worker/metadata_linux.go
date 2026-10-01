//go:build linux && n1clipboarddiagnostic && !n1candidate

package worker

import "syscall"

func nativeMeta(s *syscall.Stat_t) (int64, uint32) { return s.Ctim.Sec*1e9 + s.Ctim.Nsec, 0 }
