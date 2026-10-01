//go:build !n1clipboarddiagnostic || n1candidate

package main

import (
	"io"
	"strings"
	"testing"
)

type diagnosticForbiddenReader struct{}

func (diagnosticForbiddenReader) Read([]byte) (int, error) {
	panic("forbidden diagnostic command read input")
}
func TestN1ClipboardDiagnosticForbiddenInDefaultAndCanonical(t *testing.T) {
	for _, action := range []string{"invoke", "collect"} {
		handled, err := runInternal(t.Context(), []string{"internal", "n1-clipboard-diagnostic", action}, diagnosticForbiddenReader{}, io.Discard, nil)
		if !handled || err == nil || !strings.Contains(err.Error(), "unsupported internal command") {
			t.Fatal("diagnostic capability exposed", err)
		}
	}
}
