//go:build darwin && cgo

package clipboardhost

/*
#cgo LDFLAGS: -framework AppKit -framework Foundation
#include <stdlib.h>
int bw_clipboard_read(const char *, void **, size_t *);
void *bw_clipboard_prepare(const char *, const void *, size_t);
int bw_clipboard_commit(void *);
void bw_clipboard_discard(void *);
void bw_clipboard_release(const char *);
*/
import "C"
import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"unsafe"
)

type appKitBoard struct{ name string }

func newBoard(name string) clipboardx.Pasteboard {
	return &processBoard{name: name, command: helperCommand}
}
func (b *appKitBoard) ReadText(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	name := C.CString(b.name)
	defer C.free(unsafe.Pointer(name))
	var ptr unsafe.Pointer
	var count C.size_t
	status := C.bw_clipboard_read(name, &ptr, &count)
	if ptr != nil {
		defer C.free(ptr)
	}
	if ctx.Err() != nil {
		return nil, clipboardx.ErrCancelled
	}
	switch status {
	case 1:
		return nil, clipboardx.ErrTooLarge
	case 0:
	default:
		return nil, clipboardx.ErrUnavailable
	}
	data := C.GoBytes(ptr, C.int(count))
	if err := clipboardx.Validate(data); err != nil {
		return nil, err
	}
	return data, nil
}
func (b *appKitBoard) WriteText(ctx context.Context, data []byte) (clipboardx.Outcome, error) {
	if err := clipboardx.Validate(data); err != nil {
		return clipboardx.Unchanged, err
	}
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	name := C.CString(b.name)
	defer C.free(unsafe.Pointer(name))
	var ptr unsafe.Pointer
	if len(data) > 0 {
		ptr = C.CBytes(data)
		defer C.free(ptr)
	}
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	prepared := C.bw_clipboard_prepare(name, ptr, C.size_t(len(data)))
	if prepared == nil {
		return clipboardx.Unchanged, clipboardx.ErrWrite
	}
	defer C.bw_clipboard_discard(prepared)
	if ctx.Err() != nil {
		return clipboardx.Unchanged, clipboardx.ErrCancelled
	}
	switch C.bw_clipboard_commit(prepared) {
	case 0:
		return clipboardx.Committed, nil
	case 1:
		return clipboardx.Unchanged, clipboardx.ErrWrite
	default:
		return clipboardx.Unknown, clipboardx.ErrUnknown
	}
}

// Tests use a uniquely named private pasteboard; this never releases general.
func releasePrivateBoard(board clipboardx.Pasteboard) {
	b, ok := board.(*processBoard)
	if !ok || b.name == "" {
		return
	}
	name := C.CString(b.name)
	defer C.free(unsafe.Pointer(name))
	C.bw_clipboard_release(name)
}
