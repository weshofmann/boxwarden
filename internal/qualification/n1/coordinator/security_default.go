//go:build !darwin && !linux

package coordinator

import "syscall"

func sameNativeSecurity(a, b *syscall.Stat_t) bool { return false }
