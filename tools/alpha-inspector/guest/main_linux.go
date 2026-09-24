//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type bootReport struct {
	DiskPrefixSHA256  string   `json:"disk_prefix_sha256"`
	NetworkInterfaces []string `json:"network_interfaces"`
	ReadOnly          bool     `json:"read_only"`
	FixtureUUID       string   `json:"fixture_uuid,omitempty"`
	FixtureContent    string   `json:"fixture_content,omitempty"`
	CopyFileSHA256    string   `json:"copy_file_sha256,omitempty"`
	CopyFileSize      int64    `json:"copy_file_size,omitempty"`
	MountOptions      []string `json:"mount_options,omitempty"`
}

func main() {
	if err := runProof(); err != nil {
		consoleLine("proof failed: " + err.Error())
	}
	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		consoleLine("poweroff failed: " + err.Error())
	}
	for {
		time.Sleep(time.Hour)
	}
}

func runProof() error {
	for _, directory := range []string{"/dev", "/proc", "/sys"} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}
	for _, mount := range []struct{ source, target, filesystem string }{
		{"devtmpfs", "/dev", "devtmpfs"},
		{"proc", "/proc", "proc"},
		{"sysfs", "/sys", "sysfs"},
	} {
		if err := syscall.Mount(mount.source, mount.target, mount.filesystem, syscall.MS_NOSUID|syscall.MS_NOEXEC, ""); err != nil {
			return fmt.Errorf("mount %s: %w", mount.filesystem, err)
		}
	}
	consoleLine("alpha inspector synthetic proof booted")
	commandLine, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	transaction, err := parseTransaction(string(commandLine))
	if err != nil {
		return err
	}
	mode, err := parseFixtureMode(string(commandLine))
	if err != nil {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for _, name := range []string{"/dev/hvc1", "/dev/vda", "/sys/block/vda/ro"} {
		for {
			if _, err := os.Stat(name); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for %s", name)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	ro, err := os.ReadFile("/sys/block/vda/ro")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(ro)) != "1" {
		return fmt.Errorf("synthetic virtio block device is not read-only")
	}
	device, err := os.Open("/dev/vda")
	if err != nil {
		return err
	}
	var prefix [4096]byte
	_, readErr := io.ReadFull(device, prefix[:])
	closeErr := device.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if mode == "zero" && prefix != [4096]byte{} {
		return fmt.Errorf("synthetic zero disk changed")
	}
	interfaces, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return err
	}
	if len(interfaces) != 1 || interfaces[0].Name() != "lo" {
		return fmt.Errorf("unexpected network interface count or name")
	}
	prefixDigest := sha256.Sum256(prefix[:])
	report := bootReport{
		DiskPrefixSHA256:  hex.EncodeToString(prefixDigest[:]),
		NetworkInterfaces: []string{"lo"},
		ReadOnly:          true,
	}
	if mode == "ext4" || mode == "copy" {
		sectors, readErr := os.ReadFile("/sys/block/vda/size")
		if readErr != nil {
			return readErr
		}
		count, parseErr := strconv.ParseUint(strings.TrimSpace(string(sectors)), 10, 64)
		if parseErr != nil || count != 64*1024*1024/512 {
			return fmt.Errorf("synthetic ext4 disk size mismatch")
		}
		device, openErr := os.Open("/dev/vda")
		if openErr != nil {
			return openErr
		}
		var superblock [1024]byte
		_, readErr = device.ReadAt(superblock[:], 1024)
		closeErr = device.Close()
		if readErr != nil || closeErr != nil {
			return fmt.Errorf("read ext4 superblock: %v, close: %v", readErr, closeErr)
		}
		uuid, err := inspectExt4Superblock(superblock[:])
		if err != nil {
			return err
		}
		if mode == "ext4" {
			if uuid != fixtureUUID {
				return fmt.Errorf("synthetic filesystem UUID mismatch")
			}
			content, err := mountAndReadExt4Fixture()
			if err != nil {
				return err
			}
			report.FixtureContent = string(content)
		} else {
			digest, size, err := mountAndHashExt4Copy()
			if err != nil {
				return err
			}
			report.CopyFileSHA256, report.CopyFileSize = digest, size
		}
		report.FixtureUUID = uuid
		report.MountOptions = []string{"ro", "noload", "nodev", "nosuid", "noexec"}
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	dataPort, err := os.OpenFile("/dev/hvc1", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := disableOutputProcessing(dataPort.Fd()); err != nil {
		_ = dataPort.Close()
		return err
	}
	if err := writeReport(dataPort, transaction, body); err != nil {
		_ = dataPort.Close()
		return err
	}
	if err := dataPort.Close(); err != nil {
		return err
	}
	consoleLine("alpha inspector synthetic proof sent")
	return nil
}

func disableOutputProcessing(fd uintptr) error {
	var settings syscall.Termios
	_, _, code := syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&settings)))
	if code != 0 {
		return fmt.Errorf("read data serial termios: %w", code)
	}
	// hvc1 is a TTY: OPOST/ONLCR can insert CR before LF anywhere in a
	// binary digest. Only this dedicated data port carries the BWEX stream.
	settings.Oflag &^= syscall.OPOST
	_, _, code = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCSETS), uintptr(unsafe.Pointer(&settings)))
	if code != 0 {
		return fmt.Errorf("set binary data serial termios: %w", code)
	}
	_, _, code = syscall.Syscall(syscall.SYS_IOCTL, fd, uintptr(syscall.TCGETS), uintptr(unsafe.Pointer(&settings)))
	if code != 0 || settings.Oflag&syscall.OPOST != 0 {
		return fmt.Errorf("verify binary data serial termios: ioctl=%v, output_flags=%#x", code, settings.Oflag)
	}
	return nil
}

func mountAndReadExt4Fixture() (content []byte, err error) {
	return withReadOnlyExt4(func(target string) ([]byte, error) {
		return readAllowedFixtureFile(target + "/proof.txt")
	})
}

func mountAndHashExt4Copy() (digest string, size int64, err error) {
	var content []byte
	content, err = withReadOnlyExt4(func(target string) ([]byte, error) {
		hash, length, readErr := readAllowedCopyFile(target + "/boxwarden-alpha-synthetic.txt")
		if readErr != nil {
			return nil, readErr
		}
		size = length
		return []byte(hash), nil
	})
	return string(content), size, err
}

func withReadOnlyExt4(read func(string) ([]byte, error)) (content []byte, err error) {
	const target = "/mnt/alpha-fixture"
	if err := os.MkdirAll(target, 0o700); err != nil {
		return nil, err
	}
	const flags = syscall.MS_RDONLY | syscall.MS_NODEV | syscall.MS_NOSUID | syscall.MS_NOEXEC
	if err := syscall.Mount("/dev/vda", target, "ext4", flags, "noload"); err != nil {
		return nil, fmt.Errorf("mount synthetic ext4 ro,noload: %w", err)
	}
	defer func() {
		if unmountErr := syscall.Unmount(target, 0); unmountErr != nil && err == nil {
			err = fmt.Errorf("unmount synthetic ext4: %w", unmountErr)
		}
	}()
	var state syscall.Statfs_t
	if err := syscall.Statfs(target, &state); err != nil {
		return nil, err
	}
	// Linux statfs ST_RDONLY|ST_NOSUID|ST_NODEV|ST_NOEXEC. The noload
	// guarantee comes from the exact mount data above and the RO block device.
	if uint64(state.Flags)&0x0f != 0x0f {
		return nil, fmt.Errorf("synthetic ext4 mount flags are not restrictive")
	}
	return read(target)
}

func consoleLine(message string) {
	if len(message) > 300 {
		message = message[:300]
	}
	for _, target := range []string{"/dev/hvc0", "/dev/console"} {
		file, err := os.OpenFile(target, os.O_WRONLY, 0)
		if err == nil {
			_, _ = fmt.Fprintln(file, message)
			_ = file.Close()
			return
		}
	}
}
