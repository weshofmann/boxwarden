package supervisor

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A restrictive operator umask must not make an owned generation unusable.
func TestGenerationAdmitsTartSocketWithRestrictivePermissions(t *testing.T) {
	for _, mode := range []os.FileMode{0o700, 0o750, 0o755, 0o770, 0o777, 0o600} {
		t.Run(fmt.Sprintf("%o", mode), func(t *testing.T) {
			request := minimalRequest(t)
			if _, _, err := publishOrAdmitRequest(request); err != nil {
				t.Fatal(err)
			}
			scratch := filepath.Join(request.RuntimeDirectory, "tart")
			if err := os.Mkdir(scratch, 0o700); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(scratch, "control.sock")
			listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			if err := os.Chmod(socket, mode); err != nil {
				t.Fatal(err)
			}
			_, err = admitGeneration(request)
			wantValid := mode == 0o700 || mode == 0o750 || mode == 0o755
			if (err == nil) != wantValid {
				t.Fatalf("admit socket mode %o: %v, want valid %v", mode, err, wantValid)
			}
		})
	}
}

func TestGenerationAdmitsOnlyPrivateExactTartScratch(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	scratch := filepath.Join(request.RuntimeDirectory, "tart")
	if err := os.Mkdir(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err != nil {
		t.Fatalf("private empty Tart scratch rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "foreign"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil || !strings.Contains(err.Error(), "Tart scratch") {
		t.Fatalf("foreign Tart scratch entry admitted: %v", err)
	}
	if err := os.Remove(filepath.Join(scratch, "foreign")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(scratch, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil {
		t.Fatal("nonprivate Tart scratch admitted")
	}
}

func TestGenerationRejectsTartSocketAtRoot(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(request.RuntimeDirectory, "control.sock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := admitGeneration(request); err == nil || !strings.Contains(err.Error(), "unexpected generation entry") {
		t.Fatalf("root Tart socket admitted: %v", err)
	}
}
