//go:build !darwin || !cgo

package clipboardhost

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"io"
)

func RunPasteboardHelper(_ context.Context, mode, name string, _ io.Reader, _ io.Writer) error {
	if !validBoardName(name) || (mode != "read" && mode != "write") {
		return clipboardx.ErrRequest
	}
	return clipboardx.ErrUnavailable
}
