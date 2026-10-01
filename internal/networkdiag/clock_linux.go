//go:build n1diagnostic && !n1candidate && linux

package networkdiag

import (
	"syscall"
	"time"
	"unsafe"
)

func nativeClock() (ClockReading, error) {
	var ts syscall.Timespec
	_, _, e := syscall.Syscall(syscall.SYS_CLOCK_GETTIME, 7, uintptr(unsafe.Pointer(&ts)), 0)
	if e != 0 || ts.Sec < 0 || ts.Nsec < 0 || ts.Nsec >= 1000000000 || uint64(ts.Sec) > (^uint64(0)-uint64(ts.Nsec))/1000000000 {
		return ClockReading{}, ErrMetadata
	}
	n := uint64(ts.Sec)*1000000000 + uint64(ts.Nsec)
	wall := time.Now().UnixNano()
	if wall <= 0 || n == 0 {
		return ClockReading{}, ErrMetadata
	}
	return ClockReading{uint64(wall), n}, nil
}
