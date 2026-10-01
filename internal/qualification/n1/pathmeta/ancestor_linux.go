//go:build linux && n1diagnostic && n1cleanup && !n1candidate

package pathmeta

import (
	"os"
	"syscall"
)

func ancestorTimes(a, b os.FileInfo) bool {
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Mtim == y.Mtim && x.Ctim == y.Ctim
}
