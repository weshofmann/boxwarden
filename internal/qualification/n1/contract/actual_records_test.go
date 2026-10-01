package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestRetainedActualCanonicalQualificationRecords(t *testing.T) {
	for i := 0; i < 11; i++ {
		name := "sudo.json"
		if i > 0 {
			name = fmt.Sprintf("system-%02d.json", i-1)
		}
		raw, e := os.ReadFile("testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		var q Qualification
		if json.Unmarshal(raw, &q) != nil {
			t.Fatal("fixture encoding")
		}
		if _, e = ParseQualification(raw, q.Kind, q.Path, q.SHA, SHA(raw)); e != nil {
			t.Fatal(name, e)
		}
	}
}

// Optional received-byte control. Reads only named retained records; never
// inspects executable files, enumerates processes or generates qualifications.
func TestPrepared823ReceivedRecordsPureParser(t *testing.T) {
	dir := os.Getenv("N1_RECEIVED_RECORD_DIR")
	if dir == "" {
		t.Skip("explicit retained-record fixture not supplied")
	}
	for i, p := range SystemPaths {
		raw, e := os.ReadFile(fmt.Sprintf("%s/system-%04d.json", dir, i))
		if e != nil {
			t.Fatal(i, e)
		}
		var q Qualification
		if json.Unmarshal(raw, &q) != nil {
			t.Fatal(i, "fixture encoding")
		}
		if _, e = ParseQualification(raw, "digest", p, q.SHA, SHA(raw)); e != nil {
			t.Fatal(i, p, e)
		}
	}
}
