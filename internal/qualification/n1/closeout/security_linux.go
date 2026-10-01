//go:build linux

package closeout

import "syscall"

func sameNativeSecurity(a, b *syscall.Stat_t) bool { return a.Mtim == b.Mtim && a.Ctim == b.Ctim }

func sameNativeFlags(a, b *syscall.Stat_t) bool { return true }
