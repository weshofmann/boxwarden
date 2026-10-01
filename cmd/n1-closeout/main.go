//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package main

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/closeout"
	"os"
)

func main() {
	if closeout.Run() != nil {
		os.Stderr.WriteString("n1 closeout refused or unknown\n")
		os.Exit(1)
	}
}
