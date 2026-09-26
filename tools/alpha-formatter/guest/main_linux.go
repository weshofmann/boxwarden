//go:build linux

package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func main() {
	if err := runBoot(); err != nil {
		consoleLine("formatter failed: " + err.Error())
	}
	syscall.Sync()
	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		consoleLine("formatter poweroff failed: " + err.Error())
	}
	for {
		time.Sleep(time.Hour)
	}
}

func runBoot() error {
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
	for _, path := range []string{"/dev/hvc1", "/dev/vda"} {
		if err := waitDevice(path); err != nil {
			return err
		}
	}
	commandLine, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return err
	}
	report, err := runFormatter(context.Background(), string(commandLine), workspaceformat.FormatInLinuxGuest)
	if err != nil {
		return err
	}
	syscall.Sync()
	port, err := os.OpenFile("/dev/hvc1", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := writeSerialReport(port, report); err != nil {
		_ = port.Close()
		return err
	}
	return port.Close()
}

func waitDevice(path string) error {
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("formatter timed out waiting for %s", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func consoleLine(message string) {
	if len(message) > 300 {
		message = message[:300]
	}
	for _, path := range []string{"/dev/hvc0", "/dev/console"} {
		port, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err == nil {
			_, _ = fmt.Fprintln(port, message)
			_ = port.Close()
			return
		}
	}
}
