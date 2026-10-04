package projectx

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func privateRoot(t *testing.T) string {
	t.Helper()
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	return root
}
func fixtureSetup() Setup {
	return Setup{Version: 1, SourceRoot: "/work/source", FormatterBundle: "/work/formatter", ISOPath: "/work/image.iso", GoBinary: "/usr/local/go/bin/go"}
}
func fixtureRecord() Record {
	return Record{Version: 1, Domain: "work", Name: "dev", Base: "golden", VolumeID: "00112233-4455-6677-8899-aabbccddeeff", FilesystemUUID: "11112233-4455-6677-8899-aabbccddeeff", SizeBytes: 16 << 20}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func recordPath(root string) string { return filepath.Join(root, "projects", "dev.json") }

func TestSetupCreateOnlyRetryAndPrivatePublication(t *testing.T) {
	root := privateRoot(t)
	want := fixtureSetup()
	if e := SaveSetup(root, want); e != nil {
		t.Fatal(e)
	}
	if e := SaveSetup(root, want); e != nil {
		t.Fatalf("retry: %v", e)
	}
	changed := want
	changed.ISOPath = "/work/other.iso"
	if e := SaveSetup(root, changed); e == nil {
		t.Fatal("profile replaced")
	}
	got, e := LoadSetup(root)
	if e != nil || got != want {
		t.Fatalf("load %#v %v", got, e)
	}
	for path, mode := range map[string]os.FileMode{filepath.Join(root, "projects"): 0700, filepath.Join(root, "projects", ".setup.json"): 0600} {
		info, e := os.Lstat(path)
		if e != nil || info.Mode().Perm() != mode {
			t.Fatalf("private path %s: %v %v", path, info, e)
		}
	}
}
func TestRecordCreateLoadAndMonotonicUpdates(t *testing.T) {
	root := privateRoot(t)
	r := fixtureRecord()
	if e := Save(root, "work", r); !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("save absent: %v", e)
	}
	if e := Create(root, "work", r); e != nil {
		t.Fatal(e)
	}
	if e := Create(root, "work", r); e == nil {
		t.Fatal("duplicate create")
	}
	got, e := Load(root, "work", "dev")
	if e != nil || got != r {
		t.Fatalf("load %#v %v", got, e)
	}
	if _, e := Load(root, "personal", "dev"); e == nil {
		t.Fatal("cross-domain admitted")
	}
	r.SessionID = "22222233-4455-6677-8899-aabbccddeeff"
	r.BackendObject = "bw-work-dev"
	r.Initialized = true
	r.ImportID = "33332233-4455-6677-8899-aabbccddeeff"
	r.ImportSource = "/private/source"
	r.Imported = true
	if e := Save(root, "work", r); e != nil {
		t.Fatal(e)
	}
	if e := Save(root, "work", r); e != nil {
		t.Fatalf("retry: %v", e)
	}
	for name, mutate := range map[string]func(*Record){"base": func(r *Record) { r.Base = "other" }, "volume": func(r *Record) { r.VolumeID = "44442233-4455-6677-8899-aabbccddeeff" }, "filesystem": func(r *Record) { r.FilesystemUUID = "44442233-4455-6677-8899-aabbccddeeff" }, "size": func(r *Record) { r.SizeBytes = 32 << 20 }, "session": func(r *Record) { r.SessionID = "44442233-4455-6677-8899-aabbccddeeff" }, "backend": func(r *Record) { r.BackendObject = "other" }, "initialized": func(r *Record) { r.Initialized = false }, "imported": func(r *Record) { r.Imported = false }, "import": func(r *Record) { r.ImportID = "44442233-4455-6677-8899-aabbccddeeff" }} {
		t.Run(name, func(t *testing.T) {
			next := r
			mutate(&next)
			if e := Save(root, "work", next); e == nil {
				t.Fatal("rebound or regressed")
			}
			got, e := Load(root, "work", "dev")
			if e != nil || got != r {
				t.Fatal("rejected update changed record")
			}
		})
	}
	r.Name = "setup"
	if e := Create(root, "work", r); e != nil {
		t.Fatalf("setup name: %v", e)
	}
	if e := SaveSetup(root, fixtureSetup()); e != nil {
		t.Fatalf("profile collision: %v", e)
	}
}
func TestValidationRejectsUnsafeInput(t *testing.T) {
	for name, mutate := range map[string]func(*Record){"version": func(r *Record) { r.Version = 2 }, "domain": func(r *Record) { r.Domain = "personal" }, "name": func(r *Record) { r.Name = "../dev" }, "base": func(r *Record) { r.Base = "../base" }, "UUID": func(r *Record) { r.VolumeID = strings.ToUpper(r.VolumeID) }, "small": func(r *Record) { r.SizeBytes = (16 << 20) - 1 }, "large": func(r *Record) { r.SizeBytes = (1 << 30) + 512 }, "unaligned": func(r *Record) { r.SizeBytes = (16 << 20) + 1 }, "halfReceipt": func(r *Record) { r.SessionID = r.VolumeID }, "initializedWithoutReceipt": func(r *Record) { r.Initialized = true }, "importedWithoutImport": func(r *Record) { r.Imported = true }, "invalidImport": func(r *Record) { r.ImportID = "no" }} {
		t.Run(name, func(t *testing.T) {
			root := privateRoot(t)
			r := fixtureRecord()
			mutate(&r)
			if e := Create(root, "work", r); e == nil {
				t.Fatal("unsafe input")
			}
			if _, e := os.Stat(filepath.Join(root, "projects")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("invalid input created state")
			}
		})
	}
	for _, path := range []string{"relative", "/work/../source", "/work/./source", "/work/source/", "/work/source\x00"} {
		s := fixtureSetup()
		s.SourceRoot = path
		if e := SaveSetup(privateRoot(t), s); e == nil {
			t.Fatalf("path %q", path)
		}
	}
	s := fixtureSetup()
	s.GoBinary = "/usr/bin/notgo"
	if e := SaveSetup(privateRoot(t), s); e == nil {
		t.Fatal("non-go binary")
	}
	for _, size := range []int64{16 << 20, 1 << 30} {
		r := fixtureRecord()
		r.SizeBytes = size
		if e := Create(privateRoot(t), "work", r); e != nil {
			t.Fatalf("size %d: %v", size, e)
		}
	}
}
func TestLoadRejectsCorruptJSONWithoutChangingData(t *testing.T) {
	valid := string(mustJSON(t, fixtureRecord()))
	for name, raw := range map[string]string{"duplicate": strings.Replace(valid, `"version":1`, `"version":1,"version":1`, 1), "unknown": strings.Replace(valid, `"version":1`, `"version":1,"running":true`, 1), "wrongKey": strings.Replace(valid, `"name":"dev"`, `"name":"other"`, 1), "null": strings.Replace(valid, `"initialized":false`, `"initialized":null`, 1), "missing": strings.Replace(valid, `,"initialized":false`, "", 1), "trailing": valid + ` {}`, "large": strings.Repeat(" ", 65537)} {
		t.Run(name, func(t *testing.T) {
			root := privateRoot(t)
			if e := Create(root, "work", fixtureRecord()); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(recordPath(root), []byte(raw), 0600); e != nil {
				t.Fatal(e)
			}
			if _, e := Load(root, "work", "dev"); e == nil {
				t.Fatal("corrupt admitted")
			}
			if e := Save(root, "work", fixtureRecord()); e == nil {
				t.Fatal("corrupt overwritten")
			}
			got, e := os.ReadFile(recordPath(root))
			if e != nil || string(got) != raw {
				t.Fatal("corruption changed")
			}
		})
	}
}
func TestPrivatePathAdmissionRejectsUnsafeFilesystem(t *testing.T) {
	for _, kind := range []string{"root mode", "directory mode", "file mode", "root symlink", "directory symlink", "file symlink", "hard link"} {
		t.Run(kind, func(t *testing.T) {
			root := privateRoot(t)
			if e := Create(root, "work", fixtureRecord()); e != nil {
				t.Fatal(e)
			}
			path := recordPath(root)
			switch kind {
			case "root mode", "root symlink":
				path = root
			case "directory mode", "directory symlink":
				path = filepath.Join(root, "projects")
			}
			var e error
			switch kind {
			case "root mode", "directory mode":
				e = os.Chmod(path, 0755)
			case "file mode":
				e = os.Chmod(path, 0644)
			case "hard link":
				e = os.Link(path, path+".alias")
			default:
				e = os.Rename(path, path+".original")
				if e == nil {
					e = os.Symlink(path+".original", path)
				}
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e := Load(root, "work", "dev"); e == nil {
				t.Fatal("unsafe path admitted")
			}
			if e := Save(root, "work", fixtureRecord()); e == nil {
				t.Fatal("unsafe path overwritten")
			}
		})
	}
}

// Reserved import identity and its source must survive failed imports and may
// not be retargeted by retrying with another source or clearing the intent.
func TestImportIntentPersistsAndCannotRetargetSource(t *testing.T) {
	root := privateRoot(t)
	r := fixtureRecord()
	if err := Create(root, "work", r); err != nil {
		t.Fatal(err)
	}
	r.ImportID = "33332233-4455-6677-8899-aabbccddeeff"
	r.ImportSource = "/private/source"
	if err := Save(root, "work", r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "work", r.Name)
	if err != nil || got != r {
		t.Fatalf("intent = %#v, %v", got, err)
	}
	if err := Save(root, "work", r); err != nil {
		t.Fatalf("intent retry: %v", err)
	}
	for name, mutate := range map[string]func(*Record){
		"retarget":     func(r *Record) { r.ImportSource = "/private/other" },
		"clear source": func(r *Record) { r.ImportSource = "" },
		"clear intent": func(r *Record) { r.ImportID = ""; r.ImportSource = "" },
	} {
		t.Run(name, func(t *testing.T) {
			next := r
			mutate(&next)
			if err := Save(root, "work", next); err == nil {
				t.Fatal("reserved import source changed")
			}
			got, err := Load(root, "work", r.Name)
			if err != nil || got != r {
				t.Fatal("rejected mutation changed import intent")
			}
		})
	}
}

func TestImportIntentRejectsMissingOrNoncanonicalSource(t *testing.T) {
	for name, source := range map[string]string{"missing": "", "relative": "source", "unclean": "/private/../source", "trailing separator": "/private/source/", "NUL": "/private/source\x00"} {
		t.Run(name, func(t *testing.T) {
			root := privateRoot(t)
			r := fixtureRecord()
			r.ImportID = "33332233-4455-6677-8899-aabbccddeeff"
			r.ImportSource = source
			if err := Create(root, "work", r); err == nil {
				t.Fatal("invalid source admitted")
			}
		})
	}
	root := privateRoot(t)
	r := fixtureRecord()
	r.ImportSource = "/private/source"
	if err := Create(root, "work", r); err == nil {
		t.Fatal("source without reserved import ID admitted")
	}
}
