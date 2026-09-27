package workspaceformat

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// GuestFormatRequest is the only input the isolated Linux helper needs. The
// host path never enters the guest; the random marker identifies the newly
// attached block device before mkfs erases it.
type GuestFormatRequest struct {
	FilesystemUUID string `json:"filesystem_uuid"`
	SizeBytes      int64  `json:"size_bytes"`
	Marker         string `json:"marker"`
}

// GuestDisk is an observation from Linux sysfs, devfs, mountinfo, and swaps.
// Identity is the kernel major:minor pair, not a caller-selected device name.
type GuestDisk struct {
	Path        string
	Identity    string
	SizeBytes   int64
	Marker      string
	Block       bool
	Mounted     bool
	Partitioned bool
	Root        bool
	Swap        bool
	Stacked     bool
}

type guestEnvironment interface {
	Disks(context.Context, int64) ([]GuestDisk, error)
	Run(context.Context, string, ...string) ([]byte, error)
}

// FormatInLinuxGuest is the fixed helper entry point for a trusted, isolated
// Linux formatter VM. The host adapter must attach exactly the new raw file,
// invoke this helper there, and stop/wait/reap that VM before trusting evidence.
func FormatInLinuxGuest(ctx context.Context, request GuestFormatRequest) (FormatEvidence, error) {
	if runtime.GOOS != "linux" {
		return FormatEvidence{}, fmt.Errorf("workspace formatting requires an isolated Linux guest")
	}
	return formatInLinuxGuest(ctx, request, newLinuxGuestEnvironment())
}

// formatInLinuxGuest performs only fixed commands against the single exact
// marker-matched, unmounted, partitionless block disk. The production entry
// point lives in guest_linux.go; tests exercise this policy without devices.
func formatInLinuxGuest(ctx context.Context, request GuestFormatRequest, env guestEnvironment) (FormatEvidence, error) {
	if !validUUID(request.FilesystemUUID) || request.SizeBytes < 4096 || request.SizeBytes > 1<<43 || request.SizeBytes%512 != 0 || len(request.Marker) != 64 {
		return FormatEvidence{}, fmt.Errorf("invalid Linux guest format request")
	}
	marker, err := hex.DecodeString(request.Marker)
	if err != nil || hex.EncodeToString(marker) != request.Marker {
		return FormatEvidence{}, fmt.Errorf("invalid Linux guest disk marker")
	}
	if env == nil {
		return FormatEvidence{}, fmt.Errorf("Linux guest environment is required")
	}
	disk, err := findGuestDisk(ctx, request, env)
	if err != nil {
		return FormatEvidence{}, err
	}
	// Recheck discovery immediately before the destructive command. This does
	// not replace the VM adapter's exact single-file attachment proof.
	again, err := findGuestDisk(ctx, request, env)
	if err != nil {
		return FormatEvidence{}, fmt.Errorf("recheck Linux guest disk before mkfs: %w", err)
	}
	if again.Path != disk.Path || again.Identity != disk.Identity {
		return FormatEvidence{}, fmt.Errorf("Linux guest disk changed before mkfs")
	}
	if err := ctx.Err(); err != nil {
		return FormatEvidence{}, err
	}
	if _, err := env.Run(ctx, "/usr/sbin/mkfs.ext4", "-F", "-U", request.FilesystemUUID, "-m", "0", "-L", "boxwarden-workspace", disk.Path); err != nil {
		return FormatEvidence{}, fmt.Errorf("mkfs.ext4: %w", err)
	}
	if err := confirmGuestDisk(ctx, disk, env); err != nil {
		return FormatEvidence{}, err
	}
	if _, err := env.Run(ctx, "/usr/sbin/e2fsck", "-f", "-n", disk.Path); err != nil {
		return FormatEvidence{}, fmt.Errorf("e2fsck: %w", err)
	}
	typeOutput, err := env.Run(ctx, "/usr/sbin/blkid", "-p", "-o", "value", "-s", "TYPE", disk.Path)
	if err != nil {
		return FormatEvidence{}, fmt.Errorf("probe guest filesystem type: %w", err)
	}
	if strings.TrimSpace(string(typeOutput)) != "ext4" {
		return FormatEvidence{}, fmt.Errorf("guest filesystem type is not independently verified as ext4")
	}
	uuidOutput, err := env.Run(ctx, "/usr/sbin/blkid", "-p", "-o", "value", "-s", "UUID", disk.Path)
	if err != nil {
		return FormatEvidence{}, fmt.Errorf("probe guest ext4 UUID: %w", err)
	}
	if strings.TrimSpace(string(uuidOutput)) != request.FilesystemUUID {
		return FormatEvidence{}, fmt.Errorf("guest ext4 UUID does not match")
	}
	if err := confirmGuestDisk(ctx, disk, env); err != nil {
		return FormatEvidence{}, err
	}
	return FormatEvidence{ObservedUUID: request.FilesystemUUID, WholeDevice: true, FilesystemClean: true}, nil
}

func findGuestDisk(ctx context.Context, request GuestFormatRequest, env guestEnvironment) (GuestDisk, error) {
	disks, err := env.Disks(ctx, request.SizeBytes)
	if err != nil {
		return GuestDisk{}, err
	}
	var found *GuestDisk
	for i := range disks {
		disk := disks[i]
		if disk.SizeBytes != request.SizeBytes || disk.Marker != request.Marker {
			continue
		}
		if found != nil {
			return GuestDisk{}, fmt.Errorf("ambiguous Linux guest disk marker")
		}
		found = &disk
	}
	if found == nil {
		return GuestDisk{}, fmt.Errorf("exact Linux guest disk marker and size not found")
	}
	if err := safeGuestDisk(*found); err != nil {
		return GuestDisk{}, err
	}
	return *found, nil
}

func confirmGuestDisk(ctx context.Context, original GuestDisk, env guestEnvironment) error {
	disks, err := env.Disks(ctx, original.SizeBytes)
	if err != nil {
		return err
	}
	for _, disk := range disks {
		if disk.Path != original.Path {
			continue
		}
		if disk.Identity != original.Identity || disk.SizeBytes != original.SizeBytes {
			return fmt.Errorf("Linux guest block identity changed")
		}
		return safeGuestDisk(disk)
	}
	return fmt.Errorf("Linux guest block device disappeared")
}

func safeGuestDisk(disk GuestDisk) error {
	name := filepath.Base(disk.Path)
	if disk.Path != filepath.Join("/dev", name) || name == "." || name == ".." || !safeGuestName(name) || !validMajorMinor(disk.Identity) || !disk.Block || disk.Mounted || disk.Partitioned || disk.Root || disk.Swap || disk.Stacked {
		return fmt.Errorf("Linux guest disk is not an admitted whole, idle block device")
	}
	return nil
}

func safeGuestName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char < 'a' || char > 'z' {
			if char < 'A' || char > 'Z' {
				if char < '0' || char > '9' {
					if char != '_' && char != '-' {
						return false
					}
				}
			}
		}
	}
	return true
}

func validMajorMinor(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return false
	}
	for _, part := range parts {
		for _, char := range part {
			if char < '0' || char > '9' {
				return false
			}
		}
	}
	return true
}

type boundedGuestOutput struct {
	bytes.Buffer
	overflow bool
}

func (o *boundedGuestOutput) Write(p []byte) (int, error) {
	const maxOutput = 4096
	remaining := maxOutput - o.Len()
	if remaining < len(p) {
		o.overflow = true
	}
	if remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		_, _ = o.Buffer.Write(p[:remaining])
	}
	return len(p), nil // drain the child while retaining bounded evidence
}
