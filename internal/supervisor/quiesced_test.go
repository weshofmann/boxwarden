package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestQuiescedRequiresExactGenerationAndCleanupResiduesAbsent(t *testing.T) {
	request := minimalRequest(t)
	if _, _, err := publishOrAdmitRequest(request); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(request.RuntimeDirectory)))
	controller, err := NewExactController(root, detachedLauncher{})
	if err != nil {
		t.Fatal(err)
	}
	check := func(want bool) {
		t.Helper()
		got, err := controller.Quiesced(context.Background(), request.Binding)
		if err != nil || got != want {
			t.Fatalf("Quiesced = %t, %v; want %t", got, err, want)
		}
	}
	check(false)
	if err := os.Remove(filepath.Join(request.RuntimeDirectory, requestName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(request.RuntimeDirectory, lockName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(request.RuntimeDirectory); err != nil {
		t.Fatal(err)
	}
	check(true)
	residue := filepath.Join(filepath.Dir(request.RuntimeDirectory), "."+request.Binding.Generation+".cleanup.lock")
	if err := os.WriteFile(residue, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	check(false)
}
