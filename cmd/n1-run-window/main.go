//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package main

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/coordinator"
	"os"
)

func main() {
	if coordinator.Run() != nil {
		os.Stderr.WriteString("n1 coordinator refused or unknown\n")
		os.Exit(1)
	}
	os.Exit(contract.PlannedExit)
}
