// Package hostx defines trusted-host prerequisite policy without depending on a
// VM backend.  Backends consume an already-validated qualified toolchain.
package hostx

import "path/filepath"

const (
	QualifiedPlatform = "darwin"
	QualifiedMacOS    = "26.6.2"
	QualifiedArch     = "arm64"

	TartVersion          = "2.32.1"
	TartExecutableSHA256 = "05b65d5c14e8b41e8e44b6d9fd1278de4bedbc8b735d9b99f3c748f76f75862d"
	TartArchiveSHA256    = "8554ab4f7fc12afe52f9b7e3093a935673cbac737a83973d2db7a0683c814529"

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

func qualifiedTart(identity ToolIdentity) bool {
	return identity.Version == TartVersion && identity.ExecutableSHA256 == TartExecutableSHA256 && identity.ArchiveSHA256 == TartArchiveSHA256
}

func qualifiedSoftnet(identity ToolIdentity) bool {
	return identity.Path == QualifiedSoftnetPath && identity.Version == SoftnetVersion && identity.ExecutableSHA256 == SoftnetExecutableSHA256 && identity.ArchiveSHA256 == SoftnetArchiveSHA256
}
