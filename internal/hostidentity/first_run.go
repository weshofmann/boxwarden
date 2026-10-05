package hostidentity

// LocationObservation is a read-only snapshot of a prospective storage anchor.
// File IDs bind the plan to the selected directories; no UUID is supplied by UI.
type LocationObservation struct {
	MountPoint           string
	VolumeUUID           string
	FileID               uint64
	OutputVolumeUUID     string
	OutputFileID         uint64
	AvailableBytes       uint64
	CapacityBytes        uint64
	OutputAvailableBytes uint64
	OutputCapacityBytes  uint64
	ExistingEntries      int
}

func ObserveFirstRunLocation(dataLocation, configAncestor string) (LocationObservation, error) {
	return observeFirstRunLocation(dataLocation, configAncestor)
}

// MountedFirstRunLocations recommends only compatible existing APFS mount roots.
// Selection and creation remain explicit operator actions.
func MountedFirstRunLocations(configAncestor string) ([]string, error) {
	return mountedFirstRunLocations(configAncestor)
}
