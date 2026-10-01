//go:build !n1diagnostic

package hostx

func stagedToolchainEntries() string                             { return "[softnet]" }
func completeToolchainEntries() string                           { return "[manifest.json softnet]" }
func (p RootedPublisher) stageLaunchLock(string, Group) error    { return nil }
func (p RootedPublisher) validateLaunchLock(string, Group) error { return nil }
func inspectSelectedTree(DoctorInspector, Manifest, *Report)     {}

func refuseSelectedLegacyUninstall() error { return nil }
