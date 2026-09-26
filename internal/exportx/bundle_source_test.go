package exportx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectorSourcePinsIgnoreDocsOnlyCommitButRejectSourceDrift(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range inspectorStaticSources {
		write(name, "source: "+name+"\n")
	}
	write("tools/alpha-inspector/guest/main.go", "package main\n")
	write("docs/progress.md", "first\n")
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	git("init", "-q")
	git("config", "user.name", "Source Test")
	git("config", "user.email", "source-test@example.invalid")
	git("add", ".")
	git("commit", "-qm", "fixture source")
	first, err := inspectorSourceInputs(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	write("docs/progress.md", "second\n")
	if _, err := inspectorSourceInputs(context.Background(), root); err == nil {
		t.Fatal("dirty docs checkout admitted")
	}
	git("add", "docs/progress.md")
	git("commit", "-qm", "docs only")
	second, err := inspectorSourceInputs(context.Background(), root)
	if err != nil || first.commit == second.commit || !reflect.DeepEqual(first.files, second.files) {
		t.Fatalf("docs-only commit changed input pins: first=%+v second=%+v err=%v", first, second, err)
	}
	write("tools/alpha-inspector/guest/main.go", "package main\n// changed\n")
	if _, err := inspectorSourceInputs(context.Background(), root); err == nil {
		t.Fatal("dirty guest source admitted")
	}
	git("add", "tools/alpha-inspector/guest/main.go")
	git("commit", "-qm", "change guest")
	third, err := inspectorSourceInputs(context.Background(), root)
	if err != nil || reflect.DeepEqual(second.files, third.files) {
		t.Fatalf("guest change did not invalidate pins: %v", err)
	}
	write("tools/alpha-inspector/guest/extra.go", "package main\n")
	git("add", "tools/alpha-inspector/guest/extra.go")
	git("commit", "-qm", "add guest input")
	fourth, err := inspectorSourceInputs(context.Background(), root)
	if err != nil || len(fourth.files) != len(third.files)+1 {
		t.Fatalf("new guest input was absent from inventory: %v", err)
	}
}
