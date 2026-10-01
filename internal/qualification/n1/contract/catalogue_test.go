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
	for _, change := range []func(*StaticLock){func(s *StaticLock) { s.Version = 2 }, func(s *StaticLock) { s.ProtectedSudo.Kind = "digest" }, func(s *StaticLock) { s.ProtectedSudo.Path = "/bin/sudo" }, func(s *StaticLock) { s.ProtectedSudo.QualificationSHA = "" }, func(s *StaticLock) { s.OSIndex.Records-- }, func(s *StaticLock) { s.OSIndex.Pages++ }, func(s *StaticLock) { s.OSIndex.SHA = "" }, func(s *StaticLock) { s.Files = s.Files[:len(s.Files)-1] }, func(s *StaticLock) { s.Files[0].Name = "arbitrary" }, func(s *StaticLock) { s.Files[33].SHA = s.Files[0].SHA }} {
		x, _ := staticFixture()
		change(&x)
		raw, _ := json.Marshal(x)
		if _, e := ParseStaticLock(raw); e == nil {
			t.Fatal("invalid static lock admitted")
		}
	}
	for _, invalid := range [][]byte{append([]byte(" "), records.Records[0]...), bytes.Replace(records.Records[0], []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), bytes.Replace(records.Records[0], []byte(`"sha":""`), []byte(`"sha":null`), 1), bytes.Replace(records.Records[0], []byte(`"sha":""`), []byte(`"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"`), 1)} {
		xs := records
		xs.Records = append([][]byte(nil), records.Records...)
		xs.Records[0] = invalid
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

func TestPagedVersionThreeLeavesLockBudgetUnchanged(t *testing.T) {
	s, _ := staticFixture()
	s.Version = 3
	raw, _ := json.Marshal(s)
	if _, e := ParseStaticLock(raw); e != nil {
		t.Fatal("version3 static lock refused", e)
	}
	if MaxLockBytes != 65536 {
		t.Fatal("lock limit widened")
	}
}
