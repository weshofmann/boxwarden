//go:build darwin && cgo

package clipboardhost

/*
#include <stdlib.h>
void *bw_clipboard_prepare(const char *, const void *, size_t);
int bw_clipboard_commit(void *);
void bw_clipboard_discard(void *);
*/
import "C"
import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
	"unsafe"
)

// RunPasteboardHelper is the fixed private one-shot child mode. Native work is
// synchronous here; its parent owns deadline enforcement through kill and reap.
func RunPasteboardHelper(ctx context.Context, mode, name string, input io.Reader, output io.Writer) error {
	if ctx.Err() != nil || !validBoardName(name) || input == nil || output == nil {
		return clipboardx.ErrRequest
	}
	switch mode {
	case "read":
		token, err := readHelperByte(input)
		if err != nil || token != helperStart || !helperEOF(input) {
			return clipboardx.ErrRequest
		}
		data, err := (&appKitBoard{name: name}).ReadText(ctx)
		if err != nil {
			status := helperUnavailable
			if err == clipboardx.ErrTooLarge {
				status = helperTooLarge
			}
			if err == clipboardx.ErrInvalidText {
				status = helperInvalid
			}
			_, err = output.Write([]byte{status})
			return err
		}
		if _, err = output.Write([]byte{helperReady}); err != nil {
			return clipboardx.ErrWrite
		}
		return clipboardx.WriteTextFrame(output, data)
	case "write":
		data, err := readHelperText(input)
		if err != nil {
			return clipboardx.ErrFrame
		}
		cname := C.CString(name)
		defer C.free(unsafe.Pointer(cname))
		var ptr unsafe.Pointer
		if len(data) > 0 {
			ptr = C.CBytes(data)
			defer C.free(ptr)
		}
		prepared := C.bw_clipboard_prepare(cname, ptr, C.size_t(len(data)))
		if prepared == nil {
			_, err = output.Write([]byte{helperUnavailable})
			return err
		}
		defer C.bw_clipboard_discard(prepared)
		if _, err = output.Write([]byte{helperReady}); err != nil {
			return clipboardx.ErrWrite
		}
		token, err := readHelperByte(input)
		if err != nil || token != helperCommit || !helperEOF(input) || ctx.Err() != nil {
			return clipboardx.ErrCancelled
		}
		status := helperUnknown
		if C.bw_clipboard_commit(prepared) == 0 {
			status = helperCommitted
		}
		_, err = output.Write([]byte{status})
		return err
	default:
		return clipboardx.ErrRequest
	}
}
