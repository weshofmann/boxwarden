//go:build darwin && cgo

package serialx

/*
#include <stdio.h>
#include <stdlib.h>
#include <util.h>
static int boxwarden_openpty(int *master, int *slave, char *name, size_t name_len) {
  char device[1024];
  if (openpty(master, slave, device, NULL, NULL) != 0) return -1;
  if (snprintf(name, name_len, "%s", device) >= name_len) return -1;
  return 0;
}
*/
import "C"

import (
	"fmt"
	"os"
)

func allocatePTY() (*os.File, *os.File, error) {
	var master, slave C.int
	name := make([]C.char, 1024)
	if C.boxwarden_openpty(&master, &slave, &name[0], C.size_t(len(name))) != 0 {
		return nil, nil, fmt.Errorf("openpty failed")
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
