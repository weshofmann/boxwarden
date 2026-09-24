package guestproto

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const workspacePrefix = "/home/boxwarden/workspaces/"

func lookupWorkspaceOwner() (int, int, error) {
	account, err := user.Lookup("boxwarden")
	if err != nil {
		return 0, 0, err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return 0, 0, err
	}
	if uid <= 0 || gid <= 0 {
		return 0, 0, fmt.Errorf("workstation account identity is invalid")
	}
	return uid, gid, nil
}

func (b *Bootstrapper) ensureWorkspaceMounts(ctx context.Context, mounts []WorkspaceMount) error {
	for _, mount := range mounts {
		device, err := b.workspaceDevice(ctx, mount.FilesystemUUID)
		if err != nil {
			return err
		}
		existing, err := b.workspaceMount(ctx, mount.MountPath)
		if err == nil {
			if err := b.checkWorkspaceDirectory(mount.MountPath, false); err != nil {
				return err
			}
			if existing != (workspaceObservation{Source: device, FSType: "ext4", UUID: mount.FilesystemUUID, Writable: true}) {
				return fmt.Errorf("workspace mount differs from exact requested device and UUID")
			}
			if err := b.ensureWorkspaceOwner(mount.MountPath); err != nil {
				return err
			}
			continue
		}
		if err := b.workspaceMountAbsent(mount.MountPath); err != nil {
			return err
		}
		if err := b.checkWorkspaceDirectory(mount.MountPath, true); err != nil {
			return err
		}
		if _, err := b.Runner.Run(ctx, "/usr/bin/mount", "-t", "ext4", "-o", "nodev,nosuid", device, mount.MountPath); err != nil {
			return fmt.Errorf("mount workspace: %w", err)
		}
		observed, err := b.workspaceMount(ctx, mount.MountPath)
		if err != nil || observed != (workspaceObservation{Source: device, FSType: "ext4", UUID: mount.FilesystemUUID, Writable: true}) {
			return fmt.Errorf("mounted workspace does not match exact device and UUID")
		}
		if err := b.ensureWorkspaceOwner(mount.MountPath); err != nil {
			return err
		}
	}
	return nil
}

// findmnt returns the same exit status for a missing mount and other errors.
// Before mutating mount state, independently confirm that the exact target is
// absent from the kernel mount table so a failed query cannot mask an overmount.
func (b *Bootstrapper) workspaceMountAbsent(mountPath string) error {
	path := filepath.Join(b.Root, "proc/self/mountinfo")
	contents, err := os.ReadFile(path)
	if err != nil || len(contents) > 2<<20 {
		return fmt.Errorf("cannot bound kernel mount table")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			return fmt.Errorf("kernel mount table is malformed")
		}
		if fields[4] == mountPath {
			return fmt.Errorf("workspace target already has an unverified mount")
		}
	}
	return nil
}

func (b *Bootstrapper) probeWorkspaceMounts(ctx context.Context, mounts []WorkspaceMount) error {
	for _, mount := range mounts {
		if err := b.checkWorkspaceDirectory(mount.MountPath, false); err != nil {
			return err
		}
		device, err := b.workspaceDevice(ctx, mount.FilesystemUUID)
		if err != nil {
			return err
		}
		observed, err := b.workspaceMount(ctx, mount.MountPath)
		if err != nil || observed != (workspaceObservation{Source: device, FSType: "ext4", UUID: mount.FilesystemUUID, Writable: true}) {
			return fmt.Errorf("workspace mount is absent or differs from exact device and UUID")
		}
	}
	return nil
}

// Quiescing is guest cooperation for durability, never host proof of a clean
// filesystem. The stopped-volume inspector independently checks ext4 state.
func (b *Bootstrapper) quiesceWorkspaceMounts(ctx context.Context, mounts []WorkspaceMount) error {
	if err := b.probeWorkspaceMounts(ctx, mounts); err != nil {
		return fmt.Errorf("verify workspaces before shutdown: %w", err)
	}
	for i := len(mounts) - 1; i >= 0; i-- {
		mount := mounts[i]
		if _, err := b.Runner.Run(ctx, "/usr/bin/umount", "--", mount.MountPath); err != nil {
			return fmt.Errorf("unmount workspace before shutdown: %w", err)
		}
		if err := b.workspaceMountAbsent(mount.MountPath); err != nil {
			return fmt.Errorf("verify workspace unmounted before shutdown: %w", err)
		}
	}
	return nil
}

func (b *Bootstrapper) workspaceDevice(ctx context.Context, uuid string) (string, error) {
	output, err := b.Runner.Run(ctx, "/usr/sbin/blkid", "-t", "UUID="+uuid, "-o", "device")
	if err != nil {
		return "", fmt.Errorf("resolve workspace filesystem UUID: %w", err)
	}
	device := strings.TrimSuffix(string(output), "\n")
	if !validWorkspaceDevice(device) {
		return "", fmt.Errorf("workspace UUID did not resolve to one exact block device")
	}
	return device, nil
}

func validWorkspaceDevice(device string) bool {
	if !strings.HasPrefix(device, "/dev/") || filepath.Clean(device) != device || len(device) <= len("/dev/") || len(device) > 255 {
		return false
	}
	for _, c := range device[len("/dev/"):] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '/') {
			return false
		}
	}
	return true
}

type workspaceObservation struct {
	Source   string
	FSType   string
	UUID     string
	Writable bool
}

func (b *Bootstrapper) workspaceMount(ctx context.Context, mountPath string) (workspaceObservation, error) {
	output, err := b.Runner.Run(ctx, "/usr/bin/findmnt", "-n", "-o", "SOURCE,FSTYPE,UUID,VFS-OPTIONS,FS-OPTIONS", "--mountpoint", mountPath)
	if err != nil {
		return workspaceObservation{}, err
	}
	fields := strings.Fields(string(output))
	if len(fields) != 5 || !validWorkspaceDevice(fields[0]) || fields[1] != "ext4" || !validUUID(fields[2]) {
		return workspaceObservation{}, fmt.Errorf("workspace mount report is malformed")
	}
	if !writableMountOptions(fields[3]) || !writableMountOptions(fields[4]) {
		return workspaceObservation{}, fmt.Errorf("workspace mount or filesystem is read-only")
	}
	return workspaceObservation{Source: fields[0], FSType: fields[1], UUID: fields[2], Writable: true}, nil
}

func writableMountOptions(raw string) bool {
	options := strings.Split(raw, ",")
	writable := false
	for _, option := range options {
		if option == "ro" {
			return false
		}
		if option == "rw" {
			writable = true
		}
	}
	return writable
}

func (b *Bootstrapper) checkWorkspaceDirectory(mountPath string, create bool) error {
	if !validWorkspacePath(mountPath) {
		return fmt.Errorf("workspace mount path is invalid")
	}
	parts := []string{"home", "home/boxwarden", "home/boxwarden/workspaces", strings.TrimPrefix(mountPath, "/")}
	for index, part := range parts {
		path := filepath.Join(b.Root, part)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && create && index >= 2 {
			if err := os.Mkdir(path, 0o755); err != nil {
				return fmt.Errorf("create workspace mount directory: %w", err)
			}
			info, err = os.Lstat(path)
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("workspace mount path component is absent or unsafe: %s", part)
		}
	}
	return nil
}

func (b *Bootstrapper) ensureWorkspaceOwner(mountPath string) error {
	if b.workspaceOwner == nil {
		return fmt.Errorf("workstation identity lookup is unavailable")
	}
	uid, gid, err := b.workspaceOwner()
	if err != nil {
		return fmt.Errorf("resolve workstation owner: %w", err)
	}
	if uid <= 0 || gid <= 0 {
		return fmt.Errorf("workstation owner identity is invalid")
	}
	path := filepath.Join(b.Root, strings.TrimPrefix(mountPath, "/"))
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("mounted workspace root is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("mounted workspace ownership is unavailable")
	}
	if int(stat.Uid) == uid && int(stat.Gid) == gid {
		return nil
	}
	if stat.Uid != 0 || stat.Gid != 0 {
		return fmt.Errorf("mounted workspace has unexpected ownership")
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "lost+found" || !entry.IsDir() {
			return fmt.Errorf("mounted workspace with project data has unexpected ownership")
		}
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("initialize fresh workspace owner: %w", err)
	}
	return nil
}
