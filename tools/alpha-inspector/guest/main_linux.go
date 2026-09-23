//go:build linux

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

type bootReport struct {
	DiskPrefixSHA256  string   `json:"disk_prefix_sha256"`
	NetworkInterfaces []string `json:"network_interfaces"`
	ReadOnly          bool     `json:"read_only"`
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
	interfaces, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return err
	}
	if len(interfaces) != 1 || interfaces[0].Name() != "lo" {
		return fmt.Errorf("unexpected network interface count or name")
	}
	prefixDigest := sha256.Sum256(prefix[:])
	body, err := json.Marshal(bootReport{
		DiskPrefixSHA256:  hex.EncodeToString(prefixDigest[:]),
		NetworkInterfaces: []string{"lo"},
		ReadOnly:          true,
	})
	if err != nil {
		return err
	}
	dataPort, err := os.OpenFile("/dev/hvc1", os.O_WRONLY, 0)
	if err != nil {
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
