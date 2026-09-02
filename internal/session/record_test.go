package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadRecordReturnsOnlyMatchingDomainRecord(t *testing.T) {
	root := sessionRoot(t)
	writeRecord(t, root, "dev", `{"version":1,"domain":"work","name":"dev","id":"13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"},"golden_revision":"golden-r1"}`)

	record, err := LoadRecord(root, "work", "dev")
	if err != nil {
		t.Fatalf("LoadRecord() error = %v", err)
	}
	if got, want := record.GoldenRevision, "golden-r1"; got != want {
		t.Fatalf("GoldenRevision = %q, want %q", got, want)
	}
	if got, want := string(record.IntendedState), "stopped"; got != want {
		t.Fatalf("IntendedState = %q, want %q", got, want)
	}
	if _, err := LoadRecord(root, "personal", "dev"); err == nil {
		t.Fatal("LoadRecord(personal, dev) error = nil, want domain mismatch")
	}
}

func TestLoadRecordRejectsUnsafeOrMalformedState(t *testing.T) {
	root := sessionRoot(t)
	valid := `{"version":1,"domain":"work","name":"dev","id":"13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev"}}`
	for name, contents := range map[string]string{
		"unknown record field":      valid[:len(valid)-1] + `,"extra":true}`,
		"unknown backend field":     `{"version":1,"domain":"work","name":"dev","id":"13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0","mode":"clean","intended_state":"stopped","backend":{"kind":"tart","object_id":"boxwarden-work-dev","extra":true}}`,
		"unsupported version":       strings.Replace(valid, `"version":1`, `"version":2`, 1),
		"record name mismatch":      strings.Replace(valid, `"name":"dev"`, `"name":"other"`, 1),
		"invalid intended state":    strings.Replace(valid, `"intended_state":"stopped"`, `"intended_state":"ready"`, 1),
		"invalid mode":              strings.Replace(valid, `"mode":"clean"`, `"mode":"other"`, 1),
		"missing backend object id": strings.Replace(valid, `"object_id":"boxwarden-work-dev"`, `"object_id":""`, 1),
		"unsafe backend object id":  strings.Replace(valid, `"object_id":"boxwarden-work-dev"`, `"object_id":"--delete"`, 1),
		"invalid record id":         strings.Replace(valid, `"id":"13b0bf73-3bd5-4f1c-8bdc-71d50c36d6d0"`, `"id":"not-a-uuid"`, 1),
		"malformed JSON":            `{`,
	} {
		t.Run(name, func(t *testing.T) {
			writeRecord(t, root, "dev", contents)
			if _, err := LoadRecord(root, "work", "dev"); err == nil {
				t.Fatal("LoadRecord() error = nil, want rejection")
			}
		})
	}
}

func TestLoadRecordRejectsSymlinkedSessionComponents(t *testing.T) {
	root := sessionRoot(t)
	outside := t.TempDir()
	if err := os.Remove(filepath.Join(root, "sessions")); err != nil {
		t.Fatalf("Remove sessions: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "sessions")); err != nil {
		t.Fatalf("Symlink sessions: %v", err)
	}
	if _, err := LoadRecord(root, "work", "dev"); err == nil {
		t.Fatal("LoadRecord() error = nil, want sessions symlink rejection")
	}

	if err := os.Remove(filepath.Join(root, "sessions")); err != nil {
		t.Fatalf("Remove sessions symlink: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sessions"), 0o700); err != nil {
		t.Fatalf("Mkdir sessions: %v", err)
	}
	target := filepath.Join(outside, "record.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("Write target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(root, "sessions", "dev.json")); err != nil {
		t.Fatalf("Symlink record: %v", err)
	}
	if _, err := LoadRecord(root, "work", "dev"); err == nil {
		t.Fatal("LoadRecord() error = nil, want record symlink rejection")
	}
}

func TestLoadRecordRejectsMissingRecord(t *testing.T) {
	if _, err := LoadRecord(sessionRoot(t), "work", "dev"); err == nil {
		t.Fatal("LoadRecord() error = nil, want missing record error")
	}
}

func sessionRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks temp dir: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("Chmod root: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "sessions"), 0o700); err != nil {
		t.Fatalf("Mkdir sessions: %v", err)
	}
	return root
}

func writeRecord(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, "sessions", name+".json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
