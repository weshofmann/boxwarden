//go:build darwin && cgo

package serialx

import (
	"context"
	"os"
	"testing"
)

// This is intentionally a narrow Darwin integration test: the remaining
// topology assertions use injected PTYs, while this proves the platform
// allocator produces the owner-private slave that Tart and Screen will open.
func TestDarwinCreateRuntimeAllocatesOwnerPrivatePTYs(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime, err := createRuntime(context.Background(), root, "generation-darwin", qualifiedScreenFact(), testRuntimeDeps(&screenStarterFake{}, systemPTYAllocator{}))
	if err != nil {
		t.Fatalf("CreateRuntime() error = %v", err)
	}
	defer runtime.Close()
	for _, endpoint := range []string{runtime.TartSlave, runtime.OperatorSlave} {
		info, err := os.Stat(endpoint)
		if err != nil {
			t.Fatalf("stat %q: %v", endpoint, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("PTY endpoint %q mode = %04o, want 0600", endpoint, info.Mode().Perm())
		}
	}
}
