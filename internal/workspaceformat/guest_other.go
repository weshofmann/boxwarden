//go:build !linux

package workspaceformat

func newLinuxGuestEnvironment() guestEnvironment { return nil }
