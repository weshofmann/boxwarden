// Package hostx defines trusted-host prerequisite policy without depending on a
// VM backend.  Backends consume an already-validated qualified toolchain.
package hostx

import "path/filepath"

const (
	QualifiedPlatform     = "darwin"
	QualifiedMacOS        = "26.6.2"
	QualifiedMacOSBuild   = "25G83"
	TrialMacOS            = "27.0.1"
	TrialMacOSBuild       = "26A434"
	QualifiedArch         = "arm64"
	ManifestVersion       = 2
	InstallRequestVersion = 1

	TartVersion          = "2.32.1"
	TartExecutableSHA256 = "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
	TartArchiveSHA256    = "8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529"

	OperatorGroupName = "boxwarden-operators"
	SoftnetMode       = 0o4550
)

var QualifiedSoftnetPath = filepath.Join("/Library/Boxwarden/toolchains/softnet", SoftnetVersion, SoftnetExecutableSHA256, "softnet")

// ToolIdentity is the immutable provenance identity recorded in the manifest.
type ToolIdentity struct {
	Path             string `json:"path"`
	Version          string `json:"version"`
	ExecutableSHA256 string `json:"executable_sha256"`
	ArchiveSHA256    string `json:"archive_sha256"`
}

// admittedPlatformFact permits only the original qualified host and the exact
// upgraded host selected for an attended qualification trial. Admission alone
// does not assert that Tart/Softnet runtime behavior has passed on macOS 27.
func admittedPlatformFact(platform PlatformFact) bool {
	return platform.OS == QualifiedPlatform && platform.Arch == QualifiedArch && admittedMacOSPair(platform.Release, platform.Build)
}

func recordedInstallationPlatform(platform, release, build string) bool {
	return platform == QualifiedPlatform && admittedMacOSPair(release, build)
}

func admittedMacOSPair(release, build string) bool {
	return (release == QualifiedMacOS && build == QualifiedMacOSBuild) ||
		(release == TrialMacOS && build == TrialMacOSBuild)
}

// A manifest records where its exact tree was installed, not the host's
// present OS. The sole admitted upgrade is the original 26 installation on
// this exact 27 host; a newer installation cannot be adopted by an older host.
func compatibleInstallationPlatform(current PlatformFact, manifest Manifest) bool {
	if !admittedPlatformFact(current) || !recordedInstallationPlatform(manifest.Platform, manifest.MacOS, manifest.MacOSBuild) {
		return false
	}
	return (manifest.MacOS == current.Release && manifest.MacOSBuild == current.Build) ||
		(current.Release == TrialMacOS && current.Build == TrialMacOSBuild &&
			manifest.MacOS == QualifiedMacOS && manifest.MacOSBuild == QualifiedMacOSBuild)
}

func qualifiedStockTart(identity ToolIdentity) bool {
	return identity.Version == TartVersion && identity.ExecutableSHA256 == TartExecutableSHA256 && identity.ArchiveSHA256 == TartArchiveSHA256
}

func qualifiedTart(identity ToolIdentity) bool { return qualifiedStockTart(identity) }

func qualifiedSoftnet(identity ToolIdentity) bool {
	return identity.Path == QualifiedSoftnetPath && identity.Version == SoftnetVersion && identity.ExecutableSHA256 == SoftnetExecutableSHA256 && identity.ArchiveSHA256 == SoftnetArchiveSHA256
}
