package clipboardhost

import (
	"context"
	"encoding/binary"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	helperStart       byte = 1
	helperReady       byte = 2
	helperCommit      byte = 3
	helperCommitted   byte = 4
	helperUnavailable byte = 5
	helperUnknown     byte = 6
	helperTooLarge    byte = 7
	helperInvalid     byte = 8
)

// processBoard confines potentially blocking native pasteboard providers to an
// exact one-shot child. Its command constructor is private; no generic exec or
// external executable authority is exposed by this adapter.
type processBoard struct {
	name         string
	command      func(context.Context, string, string) (*exec.Cmd, error)
	beforeCommit func()
}

func helperCommand(ctx context.Context, mode, name string) (*exec.Cmd, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	cmd := exec.CommandContext(ctx, path, "internal", "clipboard-pasteboard", mode, name)
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}
	return cmd, nil
}
func validBoardName(name string) bool {
	return name == "" || (len(name) <= 255 && strings.HasPrefix(name, "org.boxwarden.test.") && clipboardx.Validate([]byte(name)) == nil)
}

type helperChild struct {
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	once    sync.Once
	waitErr error
}

func (c *helperChild) close() error {
	c.once.Do(func() { c.input.Close(); c.cmd.Process.Kill(); c.waitErr = c.cmd.Wait(); c.output.Close() })
	return c.waitErr
}
func (b *processBoard) start(ctx context.Context, mode string) (*helperChild, error) {
	if ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	if !validBoardName(b.name) || b.command == nil {
		return nil, clipboardx.ErrRequest
	}
	cmd, err := b.command(ctx, mode, b.name)
	if err != nil || cmd == nil {
		return nil, clipboardx.ErrUnavailable
	}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8"}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	defer null.Close()
	cmd.Stderr = null
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, clipboardx.ErrUnavailable
	}
	if err = cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, clipboardx.ErrUnavailable
	}
	return &helperChild{cmd: cmd, input: input, output: output}, nil
}
func (b *processBoard) ReadText(parent context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, clipboardx.TransferTimeout)
	defer cancel()
	child, err := b.start(ctx, "read")
	if err != nil {
		return nil, err
	}
	defer child.close()
	if _, err = child.input.Write([]byte{helperStart}); err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	child.input.Close()
	status, err := readHelperByte(child.output)
	if ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	if err != nil {
		return nil, clipboardx.ErrUnavailable
	}
	if status != helperReady {
		return nil, statusError(status)
	}
	data, err := clipboardx.ReadTextFrame(child.output)
	waitErr := child.close()
	if ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	if err != nil {
		return nil, err
	}
	if waitErr != nil {
		return nil, clipboardx.ErrUnavailable
	}
	return data, nil
}
func (b *processBoard) WriteText(parent context.Context, data []byte) (clipboardx.Outcome, error) {
	if err := clipboardx.Validate(data); err != nil {
		return clipboardx.Unchanged, err
	}
	ctx, cancel := context.WithTimeout(parent, clipboardx.TransferTimeout)
	defer cancel()
	child, err := b.start(ctx, "write")
	if err != nil {
		return clipboardx.Unchanged, err
	}
	defer child.close()
	if err = clipboardx.WriteTextFrame(child.input, data); err != nil {
		if ctx.Err() != nil {
			return clipboardx.Unchanged, clipboardx.ErrCancelled
		}
		return clipboardx.Unchanged, clipboardx.ErrWrite
	}
	status, err := readHelperByte(child.output)
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	if err != nil || status != helperReady {
		return clipboardx.Unchanged, clipboardx.ErrWrite
	}
	if b.beforeCommit != nil {
		b.beforeCommit()
	}
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	// Once any commit token bytes may reach the child, cancellation or missing
	// acknowledgement cannot establish preservation. Never retry this request.
	if _, err = child.input.Write([]byte{helperCommit}); err != nil {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	child.input.Close()
	status, err = readHelperByte(child.output)
	if err != nil || status != helperCommitted {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	tail, err := io.ReadAll(io.LimitReader(child.output, 1))
	if err != nil || len(tail) != 0 {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	if child.close() != nil || ctx.Err() != nil {
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
	return clipboardx.Committed, nil
}
func readHelperByte(input io.Reader) (byte, error) {
	var value [1]byte
	_, err := io.ReadFull(input, value[:])
	return value[0], err
}
func statusError(status byte) error {
	switch status {
	case helperTooLarge:
		return clipboardx.ErrTooLarge
	case helperInvalid:
		return clipboardx.ErrInvalidText
	default:
		return clipboardx.ErrUnavailable
	}
}
func readHelperText(input io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(input, header[:]); err != nil {
		return nil, clipboardx.ErrFrame
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > clipboardx.MaxTextBytes {
		return nil, clipboardx.ErrTooLarge
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(input, data); err != nil {
		return nil, clipboardx.ErrFrame
	}
	if err := clipboardx.Validate(data); err != nil {
		return nil, err
	}
	return data, nil
}
func helperEOF(input io.Reader) bool {
	tail, err := io.ReadAll(io.LimitReader(input, 1))
	return err == nil && len(tail) == 0
}
