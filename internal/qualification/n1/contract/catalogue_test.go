package contract

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSlice1CanonicalCatalogueHashesAndKinds(t *testing.T) {
	s, records := staticFixture()
	raw, _ := json.Marshal(s)
	if _, e := ParseStaticLock(raw); e != nil {
		t.Fatal(e)
	}
	c, e := AdmitCatalogue(s, records)
	if e != nil {
		t.Fatal(e)
	}
	q, key, ok := c.Lookup(SudoPath)
	if !ok || q.Kind != "protected-sudo" || q.SHA != "" || key != s.ProtectedSudo.QualificationSHA {
		t.Fatal("sudo fabricated byte digest")
	}
	q.Ancestors[0].Mode = 0777
	q2, _, _ := c.Lookup(SudoPath)
	if q2.Ancestors[0].Mode != 0755 {
		t.Fatal("mutable catalogue")
	}
	for _, change := range []func(*StaticLock){func(s *StaticLock) { s.Version = 1 }, func(s *StaticLock) { s.ProtectedSudo.Kind = "digest" }, func(s *StaticLock) { s.ProtectedSudo.Path = "/bin/sudo" }, func(s *StaticLock) { s.ProtectedSudo.QualificationSHA = "" }, func(s *StaticLock) { s.SystemImages[0].Path = SudoPath }, func(s *StaticLock) { s.SystemImages[0].Kind = "protected-sudo" }, func(s *StaticLock) { s.SystemImages[0].SHA = "" }, func(s *StaticLock) { s.SystemImages[1] = s.SystemImages[0] }, func(s *StaticLock) { s.Files = s.Files[:len(s.Files)-1] }, func(s *StaticLock) { s.Files[0].Name = "arbitrary" }} {
		x, _ := staticFixture()
		change(&x)
		raw, _ := json.Marshal(x)
		if _, e := ParseStaticLock(raw); e == nil {
			t.Fatal("invalid static lock admitted")
		}
	}
	for _, invalid := range [][]byte{append([]byte(" "), records[0]...), bytes.Replace(records[0], []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(records[0], []byte(`"sha":""`), []byte(`"sha":null`), 1), bytes.Replace(records[0], []byte(`"sha":""`), []byte(`"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`), 1)} {
		xs := append([][]byte(nil), records...)
		xs[0] = invalid
		if _, e := AdmitCatalogue(s, xs); e == nil {
			t.Fatal("tampered/mixed/noncanonical record admitted")
		}
	}
	for _, change := range []func(*Qualification){func(q *Qualification) { q.Platform.Build = "wrong" }, func(q *Qualification) { q.Leaf.Nlink = 2 }, func(q *Qualification) { q.Leaf.Flags++ }, func(q *Qualification) { q.Leaf.Inode++ }, func(q *Qualification) { q.Leaf.NoACL = false }, func(q *Qualification) { q.Ancestors[1].Inode++ }, func(q *Qualification) { q.Ancestors[0].Mode = 0777 }, func(q *Qualification) { q.Ancestors[2].NoACL = false }} {
		q, _, _ := c.Lookup(SudoPath)
		change(&q)
		b, _ := json.Marshal(q)
		if _, e := ParseQualification(b, q.Kind, q.Path, q.SHA, SHA(b)); e == nil {
			t.Fatal("forged qualification with recomputed hash admitted")
		}
	}
}
