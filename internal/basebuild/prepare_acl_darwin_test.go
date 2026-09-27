//go:build darwin

package basebuild

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"testing"
)

func TestPrepareRejectsExtendedACLOnCacheRecordAndDirectory(t *testing.T) {
	request, deps, _, _, _ := prepareFixture(t)
	first, err := Prepare(context.Background(), request, deps)
	if err != nil {
		t.Fatal(err)
	}
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(request.StateRoot, "prepared", "records", first.Record.PreparationKey+".json")
	for _, path := range []string{record, filepath.Dir(record)} {
		command := exec.Command("/bin/chmod", "+a", "user:"+account.Username+" allow read", path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("add test ACL to %s: %v: %s", path, err, output)
		}
		if _, err := Prepare(context.Background(), request, deps); err == nil {
			t.Fatalf("cached metadata with ACL accepted: %s", path)
		}
		command = exec.Command("/bin/chmod", "-N", path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("remove test ACL from %s: %v: %s", path, err, output)
		}
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatal(err)
	}
}
