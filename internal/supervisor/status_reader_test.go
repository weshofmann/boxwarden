package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExactSnapshotReaderDoesNotPublishRuntimeState(t *testing.T) {
	request := minimalRequest(t)
	root := filepath.Dir(filepath.Dir(filepath.Dir(request.RuntimeDirectory)))
	reader, err := NewExactSnapshotReader(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reader.Snapshot(context.Background(), request.Binding); err == nil {
		t.Fatal("accepted a generation without a live owner")
	}
	if _, err := os.Lstat(request.RuntimeDirectory); !os.IsNotExist(err) {
		t.Fatalf("status published runtime generation: %v", err)
	}
	if _, err := NewExactSnapshotReader("relative"); err == nil {
		t.Fatal("accepted relative runtime root")
	}
}
