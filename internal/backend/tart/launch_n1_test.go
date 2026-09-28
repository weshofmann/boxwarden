//go:build n1candidate

package tart

import (
	"context"
	"os"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func TestN1LauncherAlwaysSelectsContainmentAcrossGenerations(t *testing.T) {
	for range 3 {
		generation := t.TempDir()
		if err := os.Chmod(generation, 0700); err != nil {
			t.Fatal(err)
		}
		process := &recordingProcessStarter{handle: &processHandleFake{}}
		_, err := newLauncher(validLaunchConfig(), process).Start(context.Background(), backend.StartRequest{ObjectID: "boxwarden-work-dev", SerialDevice: "/dev/ttys004", GenerationDirectory: generation})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, arg := range process.spec.args {
			if arg == "--net-softnet-block=@boxwarden-host-containment" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("generation lacks exact mandatory N1 selector: %#v", process.spec.args)
		}
	}
}
