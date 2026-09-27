//go:build linux

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/weshofmann/boxwarden/internal/workspaceformat"
)

func main() {
	if err := workspaceformat.RunGuestHelper(context.Background(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
