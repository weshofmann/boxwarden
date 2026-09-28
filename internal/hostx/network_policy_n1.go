//go:build n1candidate

package hostx

// SoftnetBlockTarget is bound to this build, never supplied by guest or config.
const SoftnetBlockTarget = "@boxwarden-host-containment"

// NetworkPolicyBuild describes selected source policy, not live enforcement evidence.
const NetworkPolicyBuild = "N1 candidate; not host-qualified"

// Exact locally reproduced candidate; no runtime artifact override is accepted.
const (
	SoftnetVersion          = "0.19.0-boxwarden-n1.1"
	SoftnetExecutableSHA256 = "064206d28d82b86093244114f44f726f4f5967575a9b298a7123bf0beb740ef0"
	SoftnetArchiveSHA256    = "e06a722dfc9ab998f99144adc88ff9b03cd3356d1d1b62cb3c692cd4731b458e"
)
