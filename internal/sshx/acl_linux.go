//go:build linux

package sshx

// Linux is supported for deterministic CI. M1A's extended-ACL admission is a
// Darwin-specific host policy, so no Darwin ACL grant can exist on this host.
type osPrivateACLInspector struct{}

func (osPrivateACLInspector) HasExtendedACL(string) (bool, error) { return false, nil }
