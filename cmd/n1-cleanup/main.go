//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package main

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1"
	"os"
)

func main() {
	if n1.RunCleanup() != nil {
		os.Stderr.WriteString("n1 cleanup refused or unknown\n")
		os.Exit(1)
	}
}
