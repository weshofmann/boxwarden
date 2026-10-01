//go:build darwin

package closeout

import "syscall"

func sameNativeSecurity(a, b *syscall.Stat_t) bool {
	return a.Mtimespec == b.Mtimespec && a.Ctimespec == b.Ctimespec && a.Flags == b.Flags
}

func sameNativeFlags(a, b *syscall.Stat_t) bool { return a.Flags == b.Flags }
