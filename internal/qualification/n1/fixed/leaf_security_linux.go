//go:build linux

package fixed

import "syscall"

func sameNativeSecurity(a, b *syscall.Stat_t) bool { return a.Mtim == b.Mtim && a.Ctim == b.Ctim }
