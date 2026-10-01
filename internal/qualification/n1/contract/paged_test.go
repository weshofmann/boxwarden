package contract

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func rebindFixturePages(s *StaticLock, in *CatalogueInputs) {
	var x OSIndexRecord
	if json.Unmarshal(in.Index, &x) != nil {
		panic("fixture")
	}
	for i, b := range in.Pages {
		x.Pages[i].SHA = SHA(b)
	}
	x.RejectionsSHA = SHA(in.Rejections)
	in.Index, _ = json.Marshal(x)
	s.OSIndex.SHA = SHA(in.Index)
}
func changeFixturePage(in *CatalogueInputs, i int, f func(*OSPage)) {
	var p OSPage
	if json.Unmarshal(in.Pages[i], &p) != nil {
		panic("fixture")
	}
	f(&p)
	in.Pages[i], _ = json.Marshal(p)
}
func TestPagedCatalogueComplete823AndSemanticRefusal(t *testing.T) {
	for _, mode := range []string{"positive", "index-hash", "index-version", "index-count", "index-page-count", "index-ordinal", "index-page-records", "index-extra", "missing-page", "extra-page", "page-hash", "page-version", "page-ordinal", "entry-ordinal", "entry-path", "entry-alias", "entry-SHA", "entry-record-SHA", "entry-order", "entry-duplicate", "missing-entry", "extra-entry", "missing-record", "extra-record", "record-hash", "record-path", "record-platform", "record-acl", "record-mode", "ancestor-mode", "rejection-hash", "empty-rejections", "legacy-inline"} {
		t.Run(mode, func(t *testing.T) {
			s, in := staticFixture()
			index := func(f func(*OSIndexRecord)) {
				var x OSIndexRecord
				json.Unmarshal(in.Index, &x)
				f(&x)
				in.Index, _ = json.Marshal(x)
				s.OSIndex.SHA = SHA(in.Index)
			}
			entry := func(f func(*OSImageEntry)) {
				changeFixturePage(&in, 0, func(p *OSPage) { f(&p.Entries[0]) })
				rebindFixturePages(&s, &in)
			}
			record := func(f func(*Qualification)) {
				var q Qualification
				json.Unmarshal(in.Records[1], &q)
				f(&q)
				in.Records[1], _ = json.Marshal(q)
				entry(func(x *OSImageEntry) { x.QualificationSHA = SHA(in.Records[1]) })
			}
			switch mode {
			case "index-hash":
				s.OSIndex.SHA = strings.Repeat("f", 64)
			case "index-version":
				index(func(x *OSIndexRecord) { x.Version++ })
			case "index-count":
				index(func(x *OSIndexRecord) { x.Records-- })
			case "index-page-count":
				index(func(x *OSIndexRecord) { x.Pages = x.Pages[:6] })
			case "index-ordinal":
				index(func(x *OSIndexRecord) { x.Pages[0].Ordinal++ })
			case "index-page-records":
				index(func(x *OSIndexRecord) { x.Pages[6].Records = 65 })
			case "index-extra":
				in.Index = bytes.Replace(in.Index, []byte(`"version":1`), []byte(`"version":1,"extra":0`), 1)
				s.OSIndex.SHA = SHA(in.Index)
			case "missing-page":
				in.Pages = in.Pages[:6]
			case "extra-page":
				in.Pages = append(in.Pages, in.Pages[6])
			case "page-hash":
				in.Pages[0] = append(in.Pages[0], ' ')
			case "page-version":
				changeFixturePage(&in, 0, func(p *OSPage) { p.Version++ })
				rebindFixturePages(&s, &in)
			case "page-ordinal":
				changeFixturePage(&in, 0, func(p *OSPage) { p.Ordinal++ })
				rebindFixturePages(&s, &in)
			case "entry-ordinal":
				entry(func(x *OSImageEntry) { x.Ordinal++ })
			case "entry-path":
				entry(func(x *OSImageEntry) { x.Path = "/unknown/image" })
			case "entry-alias":
				entry(func(x *OSImageEntry) {
					x.Path = "/System/Library/Frameworks/CoreDisplay.framework/Resources/WindowServer"
				})
			case "entry-SHA":
				entry(func(x *OSImageEntry) { x.SHA = "" })
			case "entry-record-SHA":
				entry(func(x *OSImageEntry) { x.QualificationSHA = "" })
			case "entry-order":
				changeFixturePage(&in, 0, func(p *OSPage) { p.Entries[0], p.Entries[1] = p.Entries[1], p.Entries[0] })
				rebindFixturePages(&s, &in)
			case "entry-duplicate":
				changeFixturePage(&in, 0, func(p *OSPage) { p.Entries[1] = p.Entries[0] })
				rebindFixturePages(&s, &in)
			case "missing-entry":
				changeFixturePage(&in, 0, func(p *OSPage) { p.Entries = p.Entries[:127] })
				rebindFixturePages(&s, &in)
			case "extra-entry":
				changeFixturePage(&in, 6, func(p *OSPage) { p.Entries = append(p.Entries, p.Entries[0]) })
				rebindFixturePages(&s, &in)
			case "missing-record":
				in.Records = in.Records[:823]
			case "extra-record":
				in.Records = append(in.Records, in.Records[1])
			case "record-hash":
				in.Records[1] = append(in.Records[1], ' ')
			case "record-path":
				record(func(q *Qualification) { q.Path = "/unknown/image" })
			case "record-platform":
				record(func(q *Qualification) { q.Platform.Build = "wrong" })
			case "record-acl":
				record(func(q *Qualification) { q.Leaf.NoACL = false })
			case "record-mode":
				record(func(q *Qualification) { q.Leaf.Mode = 0555 })
			case "ancestor-mode":
				record(func(q *Qualification) { q.Ancestors[0].Mode = 0777 })
			case "rejection-hash":
				in.Rejections = append(in.Rejections, ' ')
			case "empty-rejections":
				in.Rejections = nil
				rebindFixturePages(&s, &in)
			case "legacy-inline":
				s.Version = 2
			}
			c, e := AdmitCatalogue(s, in)
			if mode == "positive" {
				if e != nil || !c.Valid() || len(c.records) != 824 {
					t.Fatal("complete catalogue", e)
				}
				var x OSIndexRecord
				json.Unmarshal(in.Index, &x)
				if len(x.Pages) != 7 || x.Pages[0].Records != 128 || x.Pages[5].Records != 128 || x.Pages[6].Records != 55 {
					t.Fatal("page topology")
				}
				for _, p := range SystemPaths {
					q, _, ok := c.Lookup(p)
					if !ok || q.Path != p {
						t.Fatal("missing canonical target", p)
					}
				}
				if _, _, ok := c.Lookup("/System/Library/Frameworks/CoreDisplay.framework/Resources/WindowServer"); ok {
					t.Fatal("declared alias adopted")
				}
				if _, _, ok := c.Lookup("/System/Volumes/Preboot/Cryptexes/OS/usr/libexec/cupsd"); ok {
					t.Fatal("excluded Cryptex target adopted")
				}
			} else if e == nil {
				t.Fatal("invalid input admitted", mode)
			}
		})
	}
}
func TestSameExecutableSHAAtDistinctPathsRequiresIndependentRecords(t *testing.T) {
	s, in := staticFixture()
	var first, second Qualification
	json.Unmarshal(in.Records[1], &first)
	json.Unmarshal(in.Records[2], &second)
	second.SHA = first.SHA
	in.Records[2], _ = json.Marshal(second)
	changeFixturePage(&in, 0, func(p *OSPage) { p.Entries[1].SHA = first.SHA; p.Entries[1].QualificationSHA = SHA(in.Records[2]) })
	rebindFixturePages(&s, &in)
	c, e := AdmitCatalogue(s, in)
	if e != nil {
		t.Fatal("independent same-digest files refused", e)
	}
	a, ka, _ := c.Lookup(first.Path)
	b, kb, _ := c.Lookup(second.Path)
	if a.Path == b.Path || a.SHA != b.SHA || ka == kb || a.Leaf.Inode == b.Leaf.Inode {
		t.Fatal("SHA-only identity")
	}
	in.Records[2] = in.Records[1]
	changeFixturePage(&in, 0, func(p *OSPage) { p.Entries[1].QualificationSHA = SHA(in.Records[2]) })
	rebindFixturePages(&s, &in)
	if _, e := AdmitCatalogue(s, in); e == nil {
		t.Fatal("path borrowed another qualification")
	}
}
func TestPagedCatalogueAndGenericParserBoundsRemainIndependent(t *testing.T) {
	for _, mode := range []string{"lock", "index", "page", "record", "rejections"} {
		t.Run(mode, func(t *testing.T) {
			s, in := staticFixture()
			switch mode {
			case "lock":
				raw, _ := json.Marshal(s)
				raw = append(raw, bytes.Repeat([]byte(" "), 65537-len(raw))...)
				if _, e := ParseStaticLock(raw); e == nil {
					t.Fatal("oversized lock")
				}
				return
			case "index":
				in.Index = bytes.Repeat([]byte(" "), 8193)
				s.OSIndex.SHA = SHA(in.Index)
			case "page":
				in.Pages[0] = bytes.Repeat([]byte(" "), 65537)
				rebindFixturePages(&s, &in)
			case "record":
				in.Records[1] = bytes.Repeat([]byte(" "), 8193)
				changeFixturePage(&in, 0, func(p *OSPage) { p.Entries[0].QualificationSHA = SHA(in.Records[1]) })
				rebindFixturePages(&s, &in)
			case "rejections":
				in.Rejections = bytes.Repeat([]byte(" "), 65537)
				rebindFixturePages(&s, &in)
			}
			if _, e := AdmitCatalogue(s, in); e == nil {
				t.Fatal("bound widened", mode)
			}
		})
	}
	for _, raw := range []string{"[" + strings.Repeat("0,", 256) + "0]", `"` + strings.Repeat("x", 513) + `"`, strings.Repeat("[", 9) + "0" + strings.Repeat("]", 9)} {
		var v any
		if strict([]byte(raw), 65536, &v) == nil {
			t.Fatal("generic bound widened")
		}
	}
	// <=256 members at each node, but >4096 total scalar/array nodes.
	row := "[" + strings.Repeat("0,", 255) + "0]"
	raw := "[" + strings.Repeat(row+",", 16) + row + "]"
	var v any
	if strict([]byte(raw), 65536, &v) == nil {
		t.Fatal("node budget widened")
	}
}
func TestCompileFixedCataloguePathsAndTableDigest(t *testing.T) {
	if SHA([]byte(strings.Join(SystemPaths[:], "\n")+"\n")) != "0510273b1a3a67df3b221708669f7c77cac5ff2a2f250afb9ffb318b6a718fee" {
		t.Fatal("explicit reviewed selection drift")
	}
	for _, x := range []struct {
		i    int
		want string
	}{{0, QualificationRoot + "/sudo.json"}, {1, QualificationRoot + "/system-0000.json"}, {824, ""}, {823, QualificationRoot + "/system-0822.json"}, {-1, ""}} {
		if QualificationPath(x.i) != x.want {
			t.Fatal("record path", x)
		}
	}
	if SystemPagePath(0) != QualificationRoot+"/system-page-00.json" || SystemPagePath(6) != QualificationRoot+"/system-page-06.json" || SystemPagePath(7) != "" || SystemPagePath(-1) != "" {
		t.Fatal("page path")
	}
}
