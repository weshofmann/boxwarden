//go:build darwin && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"syscall"
)

func imageTimes(st *syscall.Stat_t, x contract.ImageMetadata) bool {
	return imageTimestamp(st.Mtimespec.Sec, st.Mtimespec.Nsec, x.MtimeNS) && imageTimestamp(st.Ctimespec.Sec, st.Ctimespec.Nsec, x.CtimeNS) && st.Flags == x.Flags
}
