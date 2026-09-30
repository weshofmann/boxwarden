//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package tart

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticMACExactQualifiedConfig(t *testing.T) {
	for _, mode := range []string{"good", "duplicate", "unknown", "multicast", "symlink", "hardlink", "mode", "too-large"} {
		t.Run(mode, func(t *testing.T) {
			home, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			vm := filepath.Join(home, "vms", "synthetic")
			os.MkdirAll(vm, 0700)
			p := filepath.Join(vm, "config.json")
			raw := `{"version":1,"os":"linux","arch":"arm64","cpuCountMin":2,"cpuCount":2,"memorySizeMin":4294967296,"memorySize":4294967296,"macAddress":"02:00:00:00:00:02","display":{"width":1024,"height":768},"diskFormat":"raw"}`
			switch mode {
			case "duplicate":
				raw = `{"macAddress":"02:00:00:00:00:02","macAddress":"02:00:00:00:00:03"}`
			case "unknown":
				raw = `{"macAddress":"02:00:00:00:00:02","future":true}`
			case "multicast":
				raw = `{"macAddress":"03:00:00:00:00:02"}`
			case "too-large":
				raw = string(make([]byte, 65537))
			}
			os.WriteFile(p, []byte(raw), 0600)
			switch mode {
			case "symlink":
				os.Rename(p, p+".other")
				os.Symlink(p+".other", p)
			case "hardlink":
				os.Link(p, p+".other")
			case "mode":
				os.Chmod(p, 0666)
			}
			o := NewQualifiedObserver(nil, "/synthetic/tart", home)
			got, err := o.ObserveDiagnosticMAC(context.Background(), "synthetic")
			if mode == "good" {
				if err != nil || got.MAC != [6]uint8{2, 0, 0, 0, 0, 2} || len(got.ConfigSHA256) != 64 {
					t.Fatalf("qualified MAC=%+v err=%v", got, err)
				}
			} else if err == nil {
				t.Fatal("MAC config drift admitted")
			}
		})
	}
}
