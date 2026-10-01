//go:build !darwin && !linux

package fixed

import "syscall"

func sameNativeSecurity(a, b *syscall.Stat_t) bool { return false }
