//go:build n1clipboarddiagnostic

package clipboarddiag

import (
	"bytes"
	"strings"
	"testing"
)

func TestDiagnosticPublicationJointCanonicalWitness(t *testing.T) {
	op := fixtureOperation()
	h := strings.Repeat("a", 64)
	w := PublicationWitness{V: 1, Phase: "invoke", Header: HeaderDigest(op), Generation: GenerationDigest(op), Created: false, Bootstrap: &h, Closure: &h}
	raw, e := EncodePublication(w)
	if e != nil {
		t.Fatal(e)
	}
	want := `{"v":1,"phase":"invoke","header":"` + w.Header + `","generation":"` + w.Generation + `","created":false,"bootstrap":"` + h + `","closure":"` + h + `","collect":null}` + "\n"
	if string(raw) != want || len(raw) != 368 {
		t.Fatal("joint invoke encoding", len(raw))
	}
	t.Log("invoke maximum including LF", len(raw))
	if _, e = DecodePublication(raw, op, "invoke"); e != nil {
		t.Fatal(e)
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], append(append([]byte{}, raw...), raw...), append(append([]byte{}, raw...), 1), bytes.Replace(raw, []byte(`"v":1`), []byte(`"v":1,"v":1`), 1), bytes.Replace(raw, []byte(`"created":false,`), nil, 1), bytes.Replace(raw, []byte(`"v":1`), []byte(`"v":true`), 1), bytes.Replace(raw, []byte(`"collect":null`), []byte(`"collect":null,"extra":0`), 1), append([]byte(" "), raw...), bytes.Repeat([]byte("x"), 513)} {
		if _, e = DecodePublication(bad, op, "invoke"); e == nil {
			t.Fatal("invalid witness accepted")
		}
	}
	wrong := op
	wrong.ExpiresAt = wrong.ExpiresAt.Add(1)
	if _, e = DecodePublication(raw, wrong, "invoke"); e == nil {
		t.Fatal("original expiry was not bound")
	}
	if _, e = DecodePublication(raw, op, "collect"); e == nil {
		t.Fatal("wrong phase")
	}
	w.Phase = "collect"
	w.Bootstrap = nil
	w.Closure = nil
	w.Collect = &h
	raw, e = EncodePublication(w)
	if e != nil || len(raw) != 307 {
		t.Fatal("collect maximum", e, len(raw))
	}
	t.Log("collect maximum including LF", len(raw))
	if _, e = DecodePublication(raw, op, "collect"); e != nil {
		t.Fatal(e)
	}
	w.Created = true
	if _, e = EncodePublication(w); e == nil {
		t.Fatal("collect creation claim")
	}
}
