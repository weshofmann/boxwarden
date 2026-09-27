package basebuild

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// writePreparationPayload binds the exact guest-only recipe intent to the
// private build attempt before any installer VM is created.
func writePreparationPayload(attemptDir string, raw []byte) error {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return errors.New("guest preparation payload exceeds bound")
	}
	if err := privateStateRoot(attemptDir); err != nil {
		return err
	}
	root, err := os.OpenRoot(attemptDir)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile("recipe-prepare.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	written, writeErr := file.Write(raw)
	if writeErr == nil && written != len(raw) {
		writeErr = io.ErrShortWrite
	}
	return errors.Join(writeErr, file.Sync(), file.Close(), syncPreparedRoot(root))
}
