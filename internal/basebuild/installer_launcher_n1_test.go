//go:build n1candidate

package basebuild

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/weshofmann/boxwarden/internal/backend"
)

func TestN1InstallerCannotOmitContainment(t *testing.T) {
	launch := installerLaunchFixture(t)
	serial := &fakeInstallerSerial{endpoint: filepath.Join(launch.Request.SerialDirectory, "tart-serial")}
	var args []string
	handle, err := startInstaller(context.Background(), launch, func(context.Context, string, string) (installerSerial, error) { return serial, nil }, func(_ context.Context, s installerProcessSpec) (backend.Handle, error) {
		args = s.args
		return &fakeInstallerProcess{}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Wait(context.Background())
	count := 0
	for _, arg := range args {
		if arg == "--net-softnet-block=@boxwarden-host-containment" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("installer lacks mandatory N1 selector: %#v", args)
	}
}
