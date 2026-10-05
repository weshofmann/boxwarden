package basebuild

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weshofmann/boxwarden/internal/execx"
)

func TestOwnedScriptStreamsActualOutput(t *testing.T) {
	script := filepath.Join(t.TempDir(), "script.sh")
	if err := os.WriteFile(script, []byte("#!/bin/bash\nprintf 'synthetic stdout\\n'\nprintf 'synthetic stderr\\n' >&2\n"), 0700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err := (OSOwnedScriptRunner{Output: &out}).RunOwned(t.Context(), execx.Command{Path: "/bin/bash", Args: []string{script}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("synthetic stdout")) || !bytes.Contains(out.Bytes(), []byte("synthetic stderr")) {
		t.Fatalf("actual child progress missing: %s", out.String())
	}
}

type failedScriptOutput struct{}

func (failedScriptOutput) Write([]byte) (int, error) {
	return 0, errors.New("synthetic output failure")
}

func TestOwnedScriptOutputFailureIsReturned(t *testing.T) {
	_, err := (OSOwnedScriptRunner{Output: failedScriptOutput{}}).RunOwned(t.Context(), execx.Command{Path: "/bin/bash", Args: []string{"-c", "printf 'synthetic progress\\n'"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err == nil {
		t.Fatal("output failure was hidden")
	}
}

func TestOwnedScriptOutputIsBounded(t *testing.T) {
	_, err := (OSOwnedScriptRunner{Output: io.Discard}).RunOwned(t.Context(), execx.Command{Path: "/bin/bash", Args: []string{"-c", "/usr/bin/head -c 9437184 /dev/zero"}, Env: []string{"PATH=/usr/bin:/bin"}})
	if err == nil || !strings.Contains(err.Error(), "output exceeds 8 MiB") {
		t.Fatalf("unbounded output: %v", err)
	}
}
