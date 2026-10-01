//go:build darwin && cgo && n1diagnostic && !n1candidate

package main

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/attenduser"
	"os"
)

func main() {
	if attenduser.RunUser() != nil {
		os.Stderr.WriteString("n1 attendance refused or unknown\n")
		os.Exit(1)
	}
}
