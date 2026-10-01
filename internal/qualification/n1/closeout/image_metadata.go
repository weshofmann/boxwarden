package closeout

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"syscall"
)

func matchesImageMetadata(f os.FileInfo, x contract.ImageMetadata, dir bool) bool {
	st, ok := f.Sys().(*syscall.Stat_t)
	if !ok || f.IsDir() != dir || !dir && !f.Mode().IsRegular() {
		return false
	}
	return uint64(st.Dev) == x.Device && st.Ino == x.Inode && st.Uid == x.UID && st.Gid == x.GID && uint32(st.Mode)&07777 == x.Mode && uint64(st.Nlink) == x.Nlink && uint64(f.Size()) == x.Bytes && imageTimes(st, x)
}

func imageTimestamp(sec, nsec int64, want uint64) bool {
	if sec < 0 || nsec < 0 || nsec >= 1000000000 || uint64(sec) > (9223372036854775807-uint64(nsec))/1000000000 {
		return false
	}
	return uint64(sec)*1000000000+uint64(nsec) == want
}
