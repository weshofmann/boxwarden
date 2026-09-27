// Package clipboardx implements explicit bounded text transfers. It never logs,
// hashes, normalizes, or retains clipboard values between operations.
package clipboardx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"unicode/utf8"
)

const MaxTextBytes = 1048576

var (
	ErrTooLarge    = errors.New("clipboard text exceeds limit")
	ErrInvalidText = errors.New("clipboard text is not valid UTF-8 text")
	ErrRead        = errors.New("clipboard source read failed")
	ErrWrite       = errors.New("clipboard destination write failed")
	ErrFrame       = errors.New("invalid clipboard frame")
)

// Validate accepts empty text, but rejects non-text bytes. It does not modify data.
func Validate(data []byte) error {
	if len(data) > MaxTextBytes {
		return ErrTooLarge
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return ErrInvalidText
	}
	return nil
}

// ReadText consumes through EOF, with at most one byte beyond the text limit.
// Reader errors are deliberately replaced with payload-free sentinels.
func ReadText(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrRead
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxTextBytes+1))
	if len(data) > MaxTextBytes {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, ErrRead
	}
	if err = Validate(data); err != nil {
		return nil, err
	}
	return data, nil
}

// ReadTextFrame reads one big-endian uint32 length and exact raw text bytes,
// followed by EOF. A stream carrying metadata must parse metadata separately.
func ReadTextFrame(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrFrame
	}
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, ErrFrame
	}
	n := binary.BigEndian.Uint32(header[:])
	if n > MaxTextBytes {
		return nil, ErrTooLarge
	}
	data := make([]byte, int(n))
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, ErrFrame
	}
	// ReadAll through a one-byte limit handles readers that temporarily return 0,nil.
	trailing, err := io.ReadAll(io.LimitReader(r, 1))
	if err != nil || len(trailing) != 0 {
		return nil, ErrFrame
	}
	if err = Validate(data); err != nil {
		return nil, err
	}
	return data, nil
}

// WriteTextFrame validates the entire value before touching the destination.
func WriteTextFrame(w io.Writer, data []byte) error {
	if err := Validate(data); err != nil {
		return err
	}
	if w == nil {
		return ErrWrite
	}
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(data)))
	if err := writeExact(w, header[:]); err != nil {
		return err
	}
	return writeExact(w, data)
}
func writeExact(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err != nil || n != len(data) {
		return ErrWrite
	}
	return nil
}
