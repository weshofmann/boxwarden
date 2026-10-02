package projectx

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const recipeDigestA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const recipeDigestB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func recipeSetupJSON(t *testing.T) []byte {
	t.Helper()
	raw := string(mustJSON(t, fixtureSetup()))
	raw = strings.Replace(raw, `"version":1`, `"version":2`, 1)
	return []byte(strings.TrimSuffix(raw, "}") + `,"openssl_path":"/tools/openssl","openssl_sha256":"` + recipeDigestA + `","xorriso_path":"/tools/xorriso","xorriso_sha256":"` + recipeDigestB + `"}`)
}
func recipeRecordJSON(t *testing.T, r Record, digest string) []byte {
	t.Helper()
	raw := string(mustJSON(t, r))
	raw = strings.Replace(raw, `"version":1`, `"version":2`, 1)
	return []byte(strings.TrimSuffix(raw, "}") + `,"recipe_intent_digest":"` + digest + `"}`)
}

// A new schema must not rewrite the JSON shape of an existing setup/project.
func TestLegacyBindingJSONShapeRemainsUnchanged(t *testing.T) {
	root := privateRoot(t)
	if err := SaveSetup(root, fixtureSetup()); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "work", fixtureRecord()); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		filepath.Join(root, "projects", setupName): "{\"version\":1,\"source_root\":\"/work/source\",\"formatter_bundle\":\"/work/formatter\",\"iso_path\":\"/work/image.iso\",\"go_binary\":\"/usr/local/go/bin/go\"}\n",
		recordPath(root): "{\"version\":1,\"domain\":\"work\",\"name\":\"dev\",\"base\":\"golden\",\"volume_id\":\"00112233-4455-6677-8899-aabbccddeeff\",\"filesystem_uuid\":\"11112233-4455-6677-8899-aabbccddeeff\",\"size_bytes\":16777216,\"session_id\":\"\",\"backend_object\":\"\",\"initialized\":false,\"import_id\":\"\",\"import_source\":\"\",\"imported\":false}\n",
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("legacy bytes %s: %q %v", path, got, err)
		}
	}
}

// Tool locators survive an explicit v1->v2 update and v2->v2 update without
// changing archived bytes or bookmarks; listing admits both history schemas.
func TestRecipeSetupUpdateRetainsToolsAndExactVersionedHistory(t *testing.T) {
	root := privateRoot(t)
	prior := fixtureSetup()
	if err := SaveSetup(root, prior); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(filepath.Join(root, "projects", setupName))
	if err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "work", fixtureRecord()); err != nil {
		t.Fatal(err)
	}
	var next Setup
	if err := json.Unmarshal(recipeSetupJSON(t), &next); err != nil {
		t.Fatal(err)
	}
	history, err := UpdateSetup(root, next)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := os.ReadFile(history)
	if err != nil || !bytes.Equal(old, archived) {
		t.Fatalf("legacy history changed: %s %v", archived, err)
	}
	got, err := LoadSetup(root)
	if err != nil || !bytes.Contains(mustJSON(t, got), []byte(`"openssl_sha256":"`+recipeDigestA+`"`)) {
		t.Fatalf("tools lost: %+v %v", got, err)
	}
	versioned, err := os.ReadFile(filepath.Join(root, "projects", setupName))
	if err != nil {
		t.Fatal(err)
	}
	next.SourceRoot = "/updated/source"
	history, err = UpdateSetup(root, next)
	if err != nil {
		t.Fatal(err)
	}
	archived, err = os.ReadFile(history)
	if err != nil || !bytes.Equal(versioned, archived) {
		t.Fatalf("v2 history changed: %s %v", archived, err)
	}
	listed, err := List(root, "work")
	if err != nil || len(listed) != 1 || listed[0] != fixtureRecord() {
		t.Fatalf("history listing %+v %v", listed, err)
	}
}

func TestRecipeSetupRejectsMissingForgedAndLegacyHiddenToolFields(t *testing.T) {
	valid := string(recipeSetupJSON(t))
	cases := map[string]string{
		"unknown":          strings.Replace(valid, `"version":2`, `"version":2,"extra":true`, 1),
		"legacy hidden":    strings.Replace(valid, `"version":2`, `"version":1`, 1),
		"null":             strings.Replace(valid, `"openssl_path":"/tools/openssl"`, `"openssl_path":null`, 1),
		"alias":            strings.Replace(valid, `"openssl_path"`, `"OpenSSL_Path"`, 1),
		"duplicate":        strings.Replace(valid, `"openssl_path":"/tools/openssl"`, `"openssl_path":"/tools/openssl","openssl_path":"/tools/openssl"`, 1),
		"relative":         strings.Replace(valid, "/tools/openssl", "tools/openssl", 1),
		"unclean":          strings.Replace(valid, "/tools/openssl", "/tools/../openssl", 1),
		"wrong executable": strings.Replace(valid, "/tools/openssl", "/tools/notopenssl", 1),
		"bad digest":       strings.Replace(valid, recipeDigestA, strings.ToUpper(recipeDigestA), 1),
	}
	for _, field := range []string{"openssl_path", "openssl_sha256", "xorriso_path", "xorriso_sha256"} {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(valid), &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, field)
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		cases["missing "+field] = string(raw)
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			root := privateRoot(t)
			if err := SaveSetup(root, fixtureSetup()); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "projects", setupName)
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadSetup(root); err == nil {
				t.Fatal("invalid setup admitted")
			}
			if _, err := UpdateSetup(root, fixtureSetup()); err == nil {
				t.Fatal("invalid setup overwritten")
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != raw {
				t.Fatal("rejected setup changed")
			}
		})
	}
}

// Once a recipe is reserved, ordinary save cannot retarget, add, or erase it.
func TestRecipeRecordRoundTripAndImmutableIntent(t *testing.T) {
	root := privateRoot(t)
	var r Record
	if err := json.Unmarshal(recipeRecordJSON(t, fixtureRecord(), recipeDigestA), &r); err != nil {
		t.Fatal(err)
	}
	if err := Create(root, "work", r); err != nil {
		t.Fatal(err)
	}
	got, err := Load(root, "work", r.Name)
	if err != nil || !bytes.Contains(mustJSON(t, got), []byte(`"recipe_intent_digest":"`+recipeDigestA+`"`)) {
		t.Fatalf("recipe binding lost %+v %v", got, err)
	}
	r.SessionID = "22222233-4455-6677-8899-aabbccddeeff"
	r.BackendObject = "boxwarden-work-owned"
	r.Initialized = true
	if err := Save(root, "work", r); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{recipeDigestB, ""} {
		var next Record
		raw := strings.Replace(string(mustJSON(t, r)), recipeDigestA, digest, 1)
		if err := json.Unmarshal([]byte(raw), &next); err != nil {
			t.Fatal(err)
		}
		if digest == "" {
			next.Version = 1
		}
		if err := Save(root, "work", next); err == nil {
			t.Fatal("ordinary save changed recipe binding")
		}
	}
	if got, err := Load(root, "work", r.Name); err != nil || got != r {
		t.Fatal("rejected mutation changed record")
	}
	legacyRoot := privateRoot(t)
	if err := Create(legacyRoot, "work", fixtureRecord()); err != nil {
		t.Fatal(err)
	}
	if err := Save(legacyRoot, "work", r); err == nil {
		t.Fatal("ordinary save introduced recipe into legacy project")
	}
}

func TestRecipeRecordRejectsMissingMalformedOrLegacyHiddenIntent(t *testing.T) {
	valid := string(recipeRecordJSON(t, fixtureRecord(), recipeDigestA))
	for name, raw := range map[string]string{
		"missing":       strings.Replace(valid, `,"recipe_intent_digest":"`+recipeDigestA+`"`, "", 1),
		"empty":         strings.Replace(valid, recipeDigestA, "", 1),
		"malformed":     strings.Replace(valid, recipeDigestA, "foreign", 1),
		"uppercase":     strings.Replace(valid, recipeDigestA, strings.ToUpper(recipeDigestA), 1),
		"legacy hidden": strings.Replace(valid, `"version":2`, `"version":1`, 1),
		"legacy empty":  strings.TrimSuffix(string(mustJSON(t, fixtureRecord())), "}") + `,"recipe_intent_digest":""}`,
		"null":          strings.Replace(valid, `"recipe_intent_digest":"`+recipeDigestA+`"`, `"recipe_intent_digest":null`, 1),
		"alias":         strings.Replace(valid, "recipe_intent_digest", "Recipe_Intent_Digest", 1),
		"duplicate":     strings.Replace(valid, `"recipe_intent_digest":"`+recipeDigestA+`"`, `"recipe_intent_digest":"`+recipeDigestA+`","recipe_intent_digest":"`+recipeDigestA+`"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			root := privateRoot(t)
			if err := Create(root, "work", fixtureRecord()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(recordPath(root), []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root, "work", "dev"); err == nil {
				t.Fatal("invalid intent admitted")
			}
			if _, err := List(root, "work"); err == nil {
				t.Fatal("invalid intent listed")
			}
			if err := Save(root, "work", fixtureRecord()); err == nil {
				t.Fatal("invalid binding overwritten")
			}
		})
	}
}

func TestRecipeSetupCreateOnlyRejectsToolRetargetAndIncompleteProfile(t *testing.T) {
	var s Setup
	if err := json.Unmarshal(recipeSetupJSON(t), &s); err != nil {
		t.Fatal(err)
	}
	root := privateRoot(t)
	if err := SaveSetup(root, s); err != nil {
		t.Fatal(err)
	}
	if err := SaveSetup(root, s); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"openssl_path", "openssl_sha256", "xorriso_path", "xorriso_sha256"} {
		fields := map[string]any{}
		if err := json.Unmarshal(mustJSON(t, s), &fields); err != nil {
			t.Fatal(err)
		}
		fields[field] = ""
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		var incomplete Setup
		if err := json.Unmarshal(raw, &incomplete); err != nil {
			t.Fatal(err)
		}
		if err := SaveSetup(privateRoot(t), incomplete); err == nil {
			t.Fatalf("missing %s admitted", field)
		}
	}
	changed := s
	changed.OpenSSLSHA256 = recipeDigestB
	if err := SaveSetup(root, changed); err == nil {
		t.Fatal("immutable setup tools silently retargeted")
	}
	if got, err := LoadSetup(root); err != nil || got != s {
		t.Fatalf("rejected retarget changed setup %+v %v", got, err)
	}
}
