//go:build linux

package workspaceformat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// linuxGuestEnvironment is usable only inside the isolated formatter guest.
// It inventories kernel block identities itself; no caller supplies /dev path.
type linuxGuestEnvironment struct{}

func newLinuxGuestEnvironment() guestEnvironment { return linuxGuestEnvironment{} }

func (linuxGuestEnvironment) Disks(ctx context.Context, expectedSize int64) ([]GuestDisk, error) {
	mounted, root, err := guestMounts()
	if err != nil {
		return nil, err
	}
	swaps, err := guestSwaps()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir("/sys/block")
	if err != nil {
		return nil, err
	}
	disks := make([]GuestDisk, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := entry.Name()
		if !safeGuestName(name) {
			return nil, fmt.Errorf("invalid sysfs block device name")
		}
		sectorsBytes, err := os.ReadFile(filepath.Join("/sys/block", name, "size"))
		if err != nil {
			return nil, err
		}
		sectors, err := strconv.ParseUint(strings.TrimSpace(string(sectorsBytes)), 10, 64)
		if err != nil || sectors > uint64(^uint64(0))/512 || sectors*512 > 1<<63-1 {
			return nil, fmt.Errorf("invalid sysfs block size")
		}
		if int64(sectors*512) != expectedSize {
			continue
		}
		disk, err := inspectLinuxDisk(name, mounted, root, swaps)
		if err != nil {
			return nil, fmt.Errorf("inspect Linux block disk %q: %w", name, err)
		}
		disks = append(disks, disk)
	}
	return disks, nil
}

func inspectLinuxDisk(name string, mounted map[string]bool, root string, swaps map[string]bool) (GuestDisk, error) {
	sysPath := filepath.Join("/sys/block", name)
	identityBytes, err := os.ReadFile(filepath.Join(sysPath, "dev"))
	if err != nil {
		return GuestDisk{}, err
	}
	identity := strings.TrimSpace(string(identityBytes))
	if !validMajorMinor(identity) {
		return GuestDisk{}, fmt.Errorf("invalid sysfs block identity")
	}
	sectorsBytes, err := os.ReadFile(filepath.Join(sysPath, "size"))
	if err != nil {
		return GuestDisk{}, err
	}
	sectors, err := strconv.ParseUint(strings.TrimSpace(string(sectorsBytes)), 10, 64)
	if err != nil || sectors > uint64(^uint64(0))/512 || sectors*512 > 1<<63-1 {
		return GuestDisk{}, fmt.Errorf("invalid sysfs block size")
	}
	path := filepath.Join("/dev", name)
	info, err := os.Lstat(path)
	if err != nil {
		return GuestDisk{}, err
	}
	block := info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0 && info.Mode()&os.ModeSymlink == 0
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !block || !ok || linuxMajorMinor(uint64(stat.Rdev)) != identity {
		return GuestDisk{}, fmt.Errorf("devfs and sysfs block identity differ")
	}
	partitioned, err := linuxHasPartitions(sysPath)
	if err != nil {
		return GuestDisk{}, err
	}
	stacked, err := linuxStacked(sysPath)
	if err != nil {
		return GuestDisk{}, err
	}
	disk := GuestDisk{Path: path, Identity: identity, SizeBytes: int64(sectors * 512), Block: true,
		Mounted: mounted[identity], Root: root == identity, Swap: swaps[identity], Partitioned: partitioned, Stacked: stacked}
	// Reading a marker is harmless even on mounted disks. An inaccessible disk
	// of the same size cannot be silently ignored by the exact-match search.
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return GuestDisk{}, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return GuestDisk{}, fmt.Errorf("block device changed while opening: %w", err)
	}
	var marker [32]byte
	if n, err := file.ReadAt(marker[:], 0); err != nil {
		return GuestDisk{}, fmt.Errorf("read complete guest disk marker: bytes=%d: %w", n, err)
	} else if n != len(marker) {
		return GuestDisk{}, fmt.Errorf("short guest disk marker read")
	}
	disk.Marker = fmt.Sprintf("%x", marker)
	return disk, nil
}

func linuxStacked(sysPath string) (bool, error) {
	for _, directory := range []string{"holders", "slaves"} {
		entries, err := os.ReadDir(filepath.Join(sysPath, directory))
		if err != nil {
			return false, err
		}
		if len(entries) != 0 {
			return true, nil
		}
	}
	return false, nil
}

func guestMounts() (map[string]bool, string, error) {
	raw, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, "", err
	}
	mounted := map[string]bool{}
	root := ""
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !validMajorMinor(fields[2]) {
			return nil, "", fmt.Errorf("invalid Linux mountinfo")
		}
		mounted[fields[2]] = true
		if fields[4] == "/" {
			root = fields[2]
		}
	}
	if root == "" {
		return nil, "", fmt.Errorf("Linux root mount was not observed")
	}
	return mounted, root, nil
}

func guestSwaps() (map[string]bool, error) {
	raw, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return nil, err
	}
	swaps := map[string]bool{}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			return nil, fmt.Errorf("invalid Linux swaps list")
		}
		info, err := os.Stat(fields[0])
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeDevice == 0 || info.Mode()&os.ModeCharDevice != 0 {
			continue // swapfile, not a whole block device
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil, fmt.Errorf("swap block identity unavailable")
		}
		swaps[linuxMajorMinor(uint64(stat.Rdev))] = true
	}
	return swaps, nil
}

func linuxMajorMinor(dev uint64) string {
	major := (dev >> 8 & 0xfff) | (dev >> 32 & 0xfffff000)
	minor := (dev & 0xff) | (dev >> 12 & 0xffffff00)
	return fmt.Sprintf("%d:%d", major, minor)
}

func (linuxGuestEnvironment) Run(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C", "LC_ALL=C"}
	cmd.Dir = "/"
	var stdout, stderr boundedGuestOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", filepath.Base(path), err, stderr.String())
	}
	if stdout.overflow || stderr.overflow {
		return nil, fmt.Errorf("%s output exceeded bounded capture", filepath.Base(path))
	}
	return stdout.Bytes(), nil
}
