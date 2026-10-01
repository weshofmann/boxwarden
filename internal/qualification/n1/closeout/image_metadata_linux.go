//go:build linux

package closeout

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"syscall"
)

func imageTimes(st *syscall.Stat_t, x contract.ImageMetadata) bool {
	return imageTimestamp(st.Mtim.Sec, st.Mtim.Nsec, x.MtimeNS) && imageTimestamp(st.Ctim.Sec, st.Ctim.Nsec, x.CtimeNS) && x.Flags == 0
}

func imageFlags(st *syscall.Stat_t, x contract.ImageMetadata) bool { return x.Flags == 0 }
