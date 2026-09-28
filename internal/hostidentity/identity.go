// Package hostidentity reads persistent host filesystem identity from a pinned
// file. Only APFS volumes advertising persistent 64-bit object IDs qualify.
package hostidentity

import "os"

type Identity struct {
	VolumeUUID string `json:"volume_uuid"`
	FileID     uint64 `json:"file_id"`
}

// Observe returns the durable APFS volume and object identity of file.
func Observe(file *os.File) (Identity, error) {
	return observe(file)
}
