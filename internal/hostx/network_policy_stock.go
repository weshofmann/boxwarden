//go:build !n1candidate && !n1diagnostic

package hostx

// SoftnetBlockTarget is bound to this build, never supplied by guest or config.
const SoftnetBlockTarget = ""

// NetworkPolicyBuild describes selected source policy, not live enforcement evidence.
const NetworkPolicyBuild = "stock ADR 015; permits gateway service access"

const (
	SoftnetVersion          = "0.19.0"
	SoftnetExecutableSHA256 = "ab333619fc8bd7277837545e49a771baa994c01c3e8c14904ae4cc4c1f37269e"
	SoftnetArchiveSHA256    = "1612e1296834aae0b6389650c7c5190add1ee8d71474e328691e67679ecda53c"
)
