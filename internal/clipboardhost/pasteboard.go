// Package clipboardhost supplies lazy operating-system clipboard access.
package clipboardhost

import "github.com/weshofmann/boxwarden/internal/clipboardx"

// New does not inspect the user's clipboard. Only explicit ReadText/WriteText
// methods touch the pasteboard after the transfer service admits a target.
func New() clipboardx.Pasteboard { return newBoard("") }
