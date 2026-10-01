//go:build darwin && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package closeout

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"syscall"
	"time"
)

type imageInfo struct {
	x   contract.ImageMetadata
	dir bool
}

func (f imageInfo) Name() string { return f.x.Path }
func (f imageInfo) Size() int64  { return int64(f.x.Bytes) }
func (f imageInfo) Mode() os.FileMode {
	if f.dir {
		return os.ModeDir | 0755
	}
	return 0755
}
func (f imageInfo) ModTime() time.Time { return time.Unix(0, int64(f.x.MtimeNS)) }
func (f imageInfo) IsDir() bool        { return f.dir }
func (f imageInfo) Sys() any {
	return &syscall.Stat_t{Dev: int32(f.x.Device), Ino: f.x.Inode, Uid: f.x.UID, Gid: f.x.GID, Mode: uint16(f.x.Mode), Nlink: uint16(f.x.Nlink), Mtimespec: syscall.Timespec{Sec: int64(f.x.MtimeNS / 1000000000), Nsec: int64(f.x.MtimeNS % 1000000000)}, Ctimespec: syscall.Timespec{Sec: int64(f.x.CtimeNS / 1000000000), Nsec: int64(f.x.CtimeNS % 1000000000)}, Flags: f.x.Flags}
}
