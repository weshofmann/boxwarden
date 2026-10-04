package importx

// Import limits bound capture, manifest re-admission, journals and pinned SFTP
// readback. Files stream through bounded buffers; these limits are not a memory
// allocation request. Keep the existing path/depth restrictions.
const (
	MaxFiles       = 4096
	MaxDirectories = 2048
	MaxFileBytes   = 64 << 20
	MaxTotalBytes  = 256 << 20
)
