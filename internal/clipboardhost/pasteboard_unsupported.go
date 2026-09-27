//go:build !darwin || !cgo

package clipboardhost

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/clipboardx"
)

type unavailableBoard struct{}

func newBoard(string) clipboardx.Pasteboard { return unavailableBoard{} }
func (unavailableBoard) ReadText(context.Context) ([]byte, error) {
	return nil, clipboardx.ErrUnavailable
}
func (unavailableBoard) WriteText(context.Context, []byte) (clipboardx.Outcome, error) {
	return clipboardx.Unchanged, clipboardx.ErrUnavailable
}
