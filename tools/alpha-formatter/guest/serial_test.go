package main

import (
	"bytes"
	"testing"
)

func TestSerialReportIsOneBoundedLine(t *testing.T) {
	var output bytes.Buffer
	if err := writeSerialReport(&output, []byte(`{"version":1,"success":false}`)); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "{\"version\":1,\"success\":false}\n"; got != want {
		t.Fatalf("serial frame %q, want %q", got, want)
	}
	if err := writeSerialReport(&output, bytes.Repeat([]byte("x"), 4096)); err == nil {
		t.Fatal("accepted oversized formatter serial report")
	}
	if err := writeSerialReport(&output, []byte("bad\nline")); err == nil {
		t.Fatal("accepted multiline formatter serial report")
	}
}
