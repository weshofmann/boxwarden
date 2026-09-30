//go:build n1clipboarddiagnostic && !n1candidate

package main

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"os"
	"runtime"
)

func main() {
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm64" || os.Geteuid() != 0 {
		os.Exit(1)
	}
	if guestproto.RunClipboardDiagnostic(context.Background(), os.Args[1:], os.Stdin, os.Stdout) != nil {
		os.Exit(1)
	}
}
