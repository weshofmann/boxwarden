// Package hostx defines trusted-host prerequisite policy without depending on a
// VM backend.  Backends consume an already-validated qualified toolchain.
package hostx

import "path/filepath"

const (
	QualifiedPlatform     = "darwin"
	QualifiedMacOS        = "26.6.2"
	QualifiedMacOSBuild   = "25G83"
	QualifiedArch         = "arm64"
	ManifestVersion       = 2
	InstallRequestVersion = 1

	TartVersion          = "2.32.1"
	TartExecutableSHA256 = "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
	TartArchiveSHA256    = "8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529"

	ControlledClipboardTartVersion            = "2.32.1-boxwarden-clipboard-r3"
	ControlledClipboardTartExecutableSHA256   = "1573e4be9a10087f8e5dce5dcc5718cfef4d5d0274c60d5e209ba1f13c49f7a6"
	ControlledClipboardTartArchiveSHA256      = "b5f487c3b092b48d23819d2828c45b96f7c84816d51b5ca9dd68f3de64fa3a64"
	ControlledClipboardTartR4Version          = "2.32.1-boxwarden-clipboard-r4"
	ControlledClipboardTartR4ExecutableSHA256 = "46e809c95260d6a264b15662bd2117eddd13b0a0ca19dcdc6bae244cc7799fc2"
	ControlledClipboardTartR4ArchiveSHA256    = "4126636c097dffaefff70c0abec116623885554419c87825b4e9e458a9f987ff"
	ControlledClipboardTartR5Version          = "2.32.1-boxwarden-clipboard-r5"
	ControlledClipboardTartR5ExecutableSHA256 = "e0047ddb7ffff0967591a1bd03374980f1775bdb4ab44ffd5b9bfb4700d7b97b"
	ControlledClipboardTartR5ArchiveSHA256    = "3a58485df6a10958e62da1fd2692c47cf54ddec0448338695b0daf327d7617bc"

	SoftnetVersion          = "0.19.0"
	SoftnetExecutableSHA256 = "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e"
	SoftnetArchiveSHA256    = "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"

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

func qualifiedPlatformFact(platform PlatformFact) bool {
	return platform.OS == QualifiedPlatform && platform.Arch == QualifiedArch && platform.Release == QualifiedMacOS && platform.Build == QualifiedMacOSBuild
}

func qualifiedStockTart(identity ToolIdentity) bool {
	return identity.Version == TartVersion && identity.ExecutableSHA256 == TartExecutableSHA256 && identity.ArchiveSHA256 == TartArchiveSHA256
}

func qualifiedSoftnet(identity ToolIdentity) bool {
	return identity.Path == QualifiedSoftnetPath && identity.Version == SoftnetVersion && identity.ExecutableSHA256 == SoftnetExecutableSHA256 && identity.ArchiveSHA256 == SoftnetArchiveSHA256
}

// SupportsControlledClipboard recognizes only the separately staged exact variant.
// It does not install the variant or alter root-owned host admission.
func SupportsControlledClipboard(identity ToolIdentity) bool {
	return canonicalAbsolute(identity.Path) &&
		((identity.Version == ControlledClipboardTartVersion && identity.ExecutableSHA256 == ControlledClipboardTartExecutableSHA256 && identity.ArchiveSHA256 == ControlledClipboardTartArchiveSHA256) ||
			(identity.Version == ControlledClipboardTartR4Version && identity.ExecutableSHA256 == ControlledClipboardTartR4ExecutableSHA256 && identity.ArchiveSHA256 == ControlledClipboardTartR4ArchiveSHA256) ||
			(identity.Version == ControlledClipboardTartR5Version && identity.ExecutableSHA256 == ControlledClipboardTartR5ExecutableSHA256 && identity.ArchiveSHA256 == ControlledClipboardTartR5ArchiveSHA256))
}

func qualifiedTart(identity ToolIdentity) bool {
	return qualifiedStockTart(identity) || SupportsControlledClipboard(identity)
}
