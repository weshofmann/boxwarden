//go:build (n1diagnostic || n1clipboarddiagnostic) && !n1candidate

package networkdiag

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func armFixture() Arm {
	return Arm{1, "ARM", "00000000-0000-4000-8000-000000000011", "00000000-0000-4000-8000-000000000030", "00000000-0000-4000-8000-000000000040", Binding{"n1qualification", "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000011", "tart", "n1-1", [4]uint8{192, 168, 64, 2}, [6]uint8{2, 0, 0, 0, 0, 2}}, Binding{"n1qualification", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000012", "tart", "n1-2", [4]uint8{192, 168, 64, 3}, [6]uint8{2, 0, 0, 0, 0, 3}}, [4]uint8{192, 168, 64, 1}, 1000, "host_backend_pinned_owner"}
}
func TestDiagnosticWireStrictCatalogue(t *testing.T) {
	a := armFixture()
	raw, _ := json.Marshal(a)
	for _, bad := range []string{strings.Replace(string(raw), `"version":1`, `"version":1,"version":1`, 1), strings.Replace(string(raw), `"version":1,`, "", 1), strings.Replace(string(raw), `"version":1`, `"Version":1`, 1), strings.Replace(string(raw), `"version":1`, `"unknown":1,"version":1`, 1), strings.Replace(string(raw), `"duration_ms":1000`, `"duration_ms":1e3`, 1), strings.Replace(string(raw), `"duration_ms":1000`, `"duration_ms":-0`, 1), strings.Replace(string(raw), `"domain":"n1qualification"`, `"domain":"n1qualification","domain":"n1qualification"`, 1), strings.Replace(string(raw), `[192,168,64,2]`, `[192,168,64,2,0]`, 1), strings.Replace(string(raw), `"ARM"`, `"\u0041RM"`, 1), string(raw) + " {}"} {
		var got Arm
		if Decode([]byte(bad), &got) == nil {
			t.Fatalf("admitted malformed fixture: %s", bad)
		}
	}
	var got Arm
	if err := Decode(raw, &got); err != nil || got != a {
		t.Fatalf("roundtrip=%v", err)
	}
}

type diagnosticPrefixOnlyReader struct {
	prefix [4]byte
	reads  int
}

func (r *diagnosticPrefixOnlyReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads != 1 {
		return 0, io.ErrUnexpectedEOF
	}
	return copy(p, r.prefix[:]), nil
}
func TestDiagnosticResponsePrefixRefusedBeforeBodyRead(t *testing.T) {
	r := &diagnosticPrefixOnlyReader{}
	binary.BigEndian.PutUint32(r.prefix[:], 4097)
	if _, err := ReadFrame(r); err == nil || r.reads != 1 {
		t.Fatalf("oversized response body read or accepted: reads=%d err=%v", r.reads, err)
	}
}
