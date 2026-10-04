// Package clipboardhost supplies lazy operating-system clipboard access.
package clipboardhost

import (
	"github.com/weshofmann/boxwarden/internal/clipboardx"
	"strings"
)

// New does not inspect the user's clipboard. Only explicit ReadText/WriteText
// methods touch the pasteboard after the transfer service admits a target.
func New() clipboardx.Pasteboard { return newBoard("") }

// ValidatePrivateBoardName admits only explicit synthetic-test boards. It never
// accepts the empty name used for the general pasteboard.
func ValidatePrivateBoardName(name string) error {
	const prefix = "org.boxwarden.test."
	if len(name) <= len(prefix) || len(name) > 255 || !strings.HasPrefix(name, prefix) {
		return clipboardx.ErrRequest
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return clipboardx.ErrRequest
		}
	}
	return nil
}

// NewPrivate is lazy and uses the same bounded native helper as New. Invalid or
// unavailable private boards never fall back to the general pasteboard.
func NewPrivate(name string) (clipboardx.Pasteboard, error) {
	if err := ValidatePrivateBoardName(name); err != nil {
		return nil, err
	}
	return newBoard(name), nil
}
