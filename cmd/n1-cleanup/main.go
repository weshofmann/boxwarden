//go:build darwin && cgo && n1diagnostic && n1cleanup && !n1candidate

package main

import (
	"context"
	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1"
	"os"
)

func main() {
	var e error
	if len(os.Args) == 1 {
		e = n1.RunCleanup()
	} else if len(os.Args) == 2 && os.Args[1] == "--stamp" {
		e = n1.RunStamp()
	} else if len(os.Args) == 2 && os.Args[1] == "--preflight" {
		e = n1.RunPreflight(readOnlyTart{observer: tart.NewQualifiedObserver(execx.OSRunner{MaxOutputBytes: 16384, StrictStderr: true}, "/Users/devel/Library/Application Support/boxwarden/toolchains/tart/2.32.1/tart.app/Contents/MacOS/tart", "/Users/devel/Library/Application Support/boxwarden/tart")})
	} else {
		e = n1.ErrRefused
	}
	if e != nil {
		os.Stderr.WriteString("n1 cleanup refused or unknown\n")
		os.Exit(1)
	}
}

// The dynamic value itself has no Creator/Stop/Delete methods.
type readOnlyTart struct{ observer backend.Observer }

func (r readOnlyTart) Observe(ctx context.Context, id string) (backend.Observation, error) {
	return r.observer.Observe(ctx, id)
}
