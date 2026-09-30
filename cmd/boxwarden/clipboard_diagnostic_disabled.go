//go:build !n1clipboarddiagnostic || n1candidate

package main

import (
	"context"
	"io"
)

func runClipboardDiagnosticInternal(context.Context, []string, io.Reader, io.Writer) (bool, error) {
	return false, nil
}
