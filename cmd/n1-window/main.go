//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package main

import (
	"os"

	"github.com/weshofmann/boxwarden/internal/qualification/n1/caller"
)

func main() {
	if caller.Run() != nil {
		os.Stderr.WriteString("n1 caller refused or unknown\n")
		os.Exit(1)
	}
}
