//go:build linux && amd64

package guestproto

import "testing"

// This fails if the amd64 implementation loses the Linux renameat2 syscall
// number required for atomic RENAME_NOREPLACE publication.
func TestLinuxAMD64Renameat2SyscallNumber(t *testing.T) {
	if linuxRenameat2Syscall != 316 {
		t.Fatalf("renameat2 syscall = %d, want 316", linuxRenameat2Syscall)
	}
}
