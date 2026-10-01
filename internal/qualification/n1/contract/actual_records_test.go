package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestRetainedActualCanonicalQualificationRecords(t *testing.T) {
	s, _ := staticFixture()
	records := [][]byte{}
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
		if _, e := ParseQualification(raw, q.Kind, q.Path, q.SHA, SHA(raw)); e != nil {
			t.Fatal(name, e)
		}
		records = append(records, raw)
		if i == 0 {
			s.ProtectedSudo = ProtectedSudo{q.Kind, q.Path, SHA(raw)}
		} else {
			s.SystemImages[i-1] = SystemImage{q.Kind, q.Path, q.SHA, SHA(raw)}
		}
	}
	if _, e := AdmitCatalogue(s, records); e != nil {
		t.Fatal(e)
	}
}
