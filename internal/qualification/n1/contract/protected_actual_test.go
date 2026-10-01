package contract

import (
	"encoding/json"
	"os"
	"testing"
)

func TestActualRetainedProtectedInventoryCanonicalAdmission(t *testing.T) {
	raw, e := os.ReadFile("testdata/protected-inventory.json")
	if e != nil {
		t.Fatal(e)
	}
	const actualSHA = "b5e63af663cd16499b77ef6ad944cd29db4258197e246db74dd1bd3052ec8cb6"
	if SHA(raw) != actualSHA {
		t.Fatal("actual record changed")
	}
	r, e := ParseProtectedInventory(raw, actualSHA)
	if e != nil || len(r.Directories) != 20 || len(r.Objects) != 12 {
		t.Fatal("actual retained record refused", e)
	}
	if _, e := ParseProtectedInventory(append(raw, '\n'), SHA(append(raw, '\n'))); e == nil {
		t.Fatal("noncanonical actual record admitted")
	}
	if _, e := ParseProtectedInventory(raw, SHA([]byte("foreign"))); e == nil {
		t.Fatal("foreign binding admitted")
	}
	r.Directories[2].Metadata.NoACL = true
	changed, _ := json.Marshal(r)
	if _, e := ParseProtectedInventory(changed, SHA(changed)); e == nil {
		t.Fatal("actual ACL record falsely claims no ACL")
	}
}
