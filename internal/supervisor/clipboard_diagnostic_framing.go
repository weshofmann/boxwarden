//go:build n1clipboarddiagnostic && !n1candidate

package supervisor

import (
	"bytes"
	"encoding/binary"
	"github.com/weshofmann/boxwarden/internal/clipboarddiag"
	"io"
)

func writeClipboardDiagnosticFrame(w io.Writer, raw []byte) error {
	if len(raw) == 0 || len(raw)+1 > clipboarddiag.MaxCollectionBytes {
		return clipboarddiag.ErrMetadata
	}
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(raw)))
	for _, part := range [][]byte{size[:], raw} {
		if _, err := io.Copy(w, bytes.NewReader(part)); err != nil {
			return clipboarddiag.ErrMetadata
		}
	}
	return nil
}
func readClipboardDiagnosticFrame(r io.Reader) ([]byte, error) {
	var size uint32
	if binary.Read(r, binary.BigEndian, &size) != nil || size == 0 || size+1 > clipboarddiag.MaxCollectionBytes {
		return nil, clipboarddiag.ErrMetadata
	}
	raw := make([]byte, size)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, clipboarddiag.ErrMetadata
	}
	var extra [1]byte
	n, err := r.Read(extra[:])
	if n != 0 || err != io.EOF {
		return nil, clipboarddiag.ErrMetadata
	}
	return raw, nil
}
