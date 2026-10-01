//go:build darwin && cgo && n1diagnostic && n1clipboarddiagnostic && !n1candidate

package main

import (
	"os"

	"github.com/weshofmann/boxwarden/internal/backend/tart"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/execx"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/worker"
)

func main() {
	c, e := config.LoadN1CurrentEnrollment()
	if e != nil {
		refuse()
	}
	h, e := c.HostAdmission()
	if e != nil {
		refuse()
	}
	o := tart.NewQualifiedObserver(execx.OSRunner{MaxOutputBytes: 1 << 20}, h.Host.TartExecutable, h.Host.TartHome)
	if worker.Run(o, o) != nil {
		refuse()
	}
}
func refuse() { os.Stderr.WriteString("n1 worker refused or unknown\n"); os.Exit(1) }
