package main

import (
	"context"
	"fmt"
	"os"

	"github.com/weshofmann/boxwarden/internal/app"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/execx"
)

func main() {
	backendAdapter := tart.New(execx.OSRunner{MaxOutputBytes: 1 << 20}, "tart")
	if err := app.Run(context.Background(), os.Args[1:], app.Options{
		Observer: backendAdapter,
		Creator:  backendAdapter,
		Output:   os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "boxwarden: %v\n", err)
		os.Exit(1)
	}
}
