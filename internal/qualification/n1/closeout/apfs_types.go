package closeout

// Narrow read-only APFS identity; F has no hostidentity enrollment graph.
type apfsIdentity struct {
	VolumeUUID string
	FileID     uint64
}
