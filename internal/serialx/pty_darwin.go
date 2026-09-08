//go:build darwin && cgo

package serialx

/*
#include <stdio.h>
#include <stdlib.h>
#include <util.h>
#include <termios.h>
#include <unistd.h>
#include <fcntl.h>
static int boxwarden_openpty(int *master, int *slave, char *name, size_t name_len) {
  char device[1024];
  if (openpty(master, slave, device, NULL, NULL) != 0) return -1;
  if (fcntl(*master, F_SETFD, FD_CLOEXEC) != 0 || fcntl(*slave, F_SETFD, FD_CLOEXEC) != 0) goto fail;
  struct termios settings;
  if (tcgetattr(*slave, &settings) != 0) goto fail;
  cfmakeraw(&settings);
  if (tcsetattr(*slave, TCSANOW, &settings) != 0) goto fail;
  if (snprintf(name, name_len, "%s", device) >= name_len) goto fail;
  return 0;
fail:
  close(*master);
  close(*slave);
  return -1;
}
*/
import "C"

import (
	"fmt"
	"os"
	"syscall"
)

func allocatePTY() (*os.File, *os.File, error) {
	var master, slave C.int
	name := make([]C.char, 1024)
	// openpty has no atomic CLOEXEC option. Coordinate with Go subprocess
	// creation until both descriptors have close-on-exec installed.
	syscall.ForkLock.RLock()
	result := C.boxwarden_openpty(&master, &slave, &name[0], C.size_t(len(name)))
	syscall.ForkLock.RUnlock()
	if result != 0 {
		return nil, nil, fmt.Errorf("openpty failed")
	}
	// Register the master with Go's poller so closing it interrupts blocked
	// reads and writes. The retained slave prevents pre-Tart hangup/EIO.
	if err := syscall.SetNonblock(int(master), true); err != nil {
		syscall.Close(int(master))
		syscall.Close(int(slave))
		return nil, nil, fmt.Errorf("set PTY master nonblocking: %w", err)
	}
	m := os.NewFile(uintptr(master), "tart-pty-master")
	s := os.NewFile(uintptr(slave), C.GoString(&name[0]))
	if err := s.Chmod(0o600); err != nil {
		m.Close()
		s.Close()
		return nil, nil, fmt.Errorf("chmod PTY slave: %w", err)
	}
	return m, s, nil
}
