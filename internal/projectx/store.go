// Package projectx stores private project bookmarks, not runtime authority.
package projectx

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/renamex"
)

const maxDocumentBytes = 64 << 10
const setupName = ".setup.json"

var syncProjectDirectory = syncDirectory

type Setup struct {
	Version         int    `json:"version"`
	SourceRoot      string `json:"source_root"`
	FormatterBundle string `json:"formatter_bundle"`
	ISOPath         string `json:"iso_path"`
	GoBinary        string `json:"go_binary"`
	OpenSSLPath     string `json:"openssl_path,omitempty"`
	OpenSSLSHA256   string `json:"openssl_sha256,omitempty"`
	XorrisoPath     string `json:"xorriso_path,omitempty"`
	XorrisoSHA256   string `json:"xorriso_sha256,omitempty"`
}
type Record struct {
	Version            int       `json:"version"`
	Domain             domain.ID `json:"domain"`
	Name               string    `json:"name"`
	Base               string    `json:"base"`
	VolumeID           string    `json:"volume_id"`
	FilesystemUUID     string    `json:"filesystem_uuid"`
	SizeBytes          int64     `json:"size_bytes"`
	SessionID          string    `json:"session_id"`
	BackendObject      string    `json:"backend_object"`
	Initialized        bool      `json:"initialized"`
	ImportID           string    `json:"import_id"`
	ImportSource       string    `json:"import_source"`
	Imported           bool      `json:"imported"`
	RecipeIntentDigest string    `json:"recipe_intent_digest,omitempty"`
	ImportSelection    string    `json:"import_selection,omitempty"`
}

// MarshalJSON keeps version1/2 byte shapes and writes every required version3
// binding, including an empty recipe digest for a legacy prepared base.
func (r Record) MarshalJSON() ([]byte, error) {
	type document Record
	if r.Version != 3 {
		return json.Marshal(document(r))
	}
	return json.Marshal(struct {
		document
		RecipeIntentDigest string `json:"recipe_intent_digest"`
		ImportSelection    string `json:"import_selection"`
	}{document: document(r), RecipeIntentDigest: r.RecipeIntentDigest, ImportSelection: r.ImportSelection})
}

// SaveSetup creates an immutable profile. An identical retry completes any
// previous publication's directory sync; consumers re-admit the stored paths.
func SaveSetup(stateRoot string, next Setup) error {
	if err := validateSetup(next); err != nil {
		return err
	}
	dir, err := openProjects(stateRoot, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	raw, _, err := readDocument(dir, setupName)
	if err == nil {
		var prior Setup
		if err := decodeSetup(raw, &prior); err != nil {
			return err
		}
		if err := validateSetup(prior); err != nil {
			return err
		}
		if prior != next {
			return fmt.Errorf("project setup is immutable; use project setup-update for an explicit admitted locator change")
		}
		return syncProjectDirectory(dir)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return publish(dir, setupName, next, nil)
}
func LoadSetup(stateRoot string) (Setup, error) {
	var s Setup
	dir, err := openProjects(stateRoot, false)
	if err != nil {
		return s, err
	}
	defer dir.Close()
	raw, _, err := readDocument(dir, setupName)
	if err != nil {
		return s, err
	}
	if err = decodeSetup(raw, &s); err != nil {
		return Setup{}, err
	}
	if err = validateSetup(s); err != nil {
		return Setup{}, err
	}
	return s, nil
}

// Create publishes allocated project intent without replacing an existing
// bookmark. The caller holds the project-specific operation lock.
func Create(stateRoot string, expectedDomain domain.ID, next Record) error {
	if err := validateRecord(expectedDomain, next); err != nil {
		return err
	}
	dir, err := openProjects(stateRoot, true)
	if err != nil {
		return err
	}
	defer dir.Close()
	return publish(dir, next.Name+".json", next, nil)
}
func Load(stateRoot string, expectedDomain domain.ID, name string) (Record, error) {
	if err := validateKey(expectedDomain, name); err != nil {
		return Record{}, err
	}
	dir, err := openProjects(stateRoot, false)
	if err != nil {
		return Record{}, err
	}
	defer dir.Close()
	r, _, err := loadRecord(dir, expectedDomain, name)
	return r, err
}

// Save advances only a pre-existing bookmark. Allocated bindings and receipt
// identities cannot change; receipt flags cannot regress. Callers validate
// receipts using the existing services before setting these advisory fields
// and hold the project-specific operation lock throughout the operation.
func Save(stateRoot string, expectedDomain domain.ID, next Record) error {
	if err := validateRecord(expectedDomain, next); err != nil {
		return err
	}
	dir, err := openProjects(stateRoot, false)
	if err != nil {
		return err
	}
	defer dir.Close()
	prior, info, err := loadRecord(dir, expectedDomain, next.Name)
	if err != nil {
		return err
	}
	selectionUpgrade := (prior.Version == 1 || prior.Version == 2) && prior.ImportID == "" && next.Version == 3 && next.ImportID != ""
	if prior.Version != next.Version && !selectionUpgrade || prior.RecipeIntentDigest != next.RecipeIntentDigest || prior.Base != next.Base || prior.VolumeID != next.VolumeID || prior.FilesystemUUID != next.FilesystemUUID || prior.SizeBytes != next.SizeBytes ||
		prior.SessionID != "" && (prior.SessionID != next.SessionID || prior.BackendObject != next.BackendObject) ||
		prior.ImportID != "" && (prior.ImportID != next.ImportID || prior.ImportSource != next.ImportSource || prior.ImportSelection != next.ImportSelection) || prior.Initialized && !next.Initialized || prior.Imported && !next.Imported {
		return fmt.Errorf("project binding is immutable or receipt state regressed")
	}
	return publish(dir, next.Name+".json", next, info)
}
func openProjects(path string, create bool) (*os.Root, error) {
	root, err := openStateRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := openChild(root, "projects", create)
	if err != nil {
		return nil, err
	}
	// A prior mkdir may have returned a sync error. Existing pathname
	// visibility does not establish that the parent entry is durable.
	if create {
		if err := syncProjectDirectory(root); err != nil {
			dir.Close()
			return nil, err
		}
	}
	return dir, nil
}
func loadRecord(dir *os.Root, d domain.ID, name string) (Record, os.FileInfo, error) {
	raw, info, err := readDocument(dir, name+".json")
	if err != nil {
		return Record{}, nil, err
	}
	var r Record
	if err := decodeRecord(raw, &r); err != nil {
		return Record{}, nil, err
	}
	if err := validateRecord(d, r); err != nil {
		return Record{}, nil, err
	}
	if r.Name != name {
		return Record{}, nil, fmt.Errorf("project name does not match file key")
	}
	return r, info, nil
}
func readDocument(dir *os.Root, name string) ([]byte, os.FileInfo, error) {
	f, err := openPrivateFile(dir, name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(raw) > maxDocumentBytes {
		return nil, nil, fmt.Errorf("project JSON exceeds 64 KiB")
	}
	return raw, info, nil
}
func publish(dir *os.Root, name string, value any, expected os.FileInfo) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return publishRaw(dir, name, raw, expected)
}

// publishRaw preserves historical documents byte-for-byte through the same
// private, exclusive publication and durability checks as current records.
func publishRaw(dir *os.Root, name string, raw []byte, expected os.FileInfo) error {
	if len(raw) > maxDocumentBytes {
		return fmt.Errorf("project JSON exceeds 64 KiB")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	temporary := name + ".tmp-" + hex.EncodeToString(nonce[:])
	f, err := dir.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer dir.Remove(temporary)
	// Close on every error path, including failed ACL admission.
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err = privateRegular(info); err != nil {
		return err
	}
	if err = checkPrivateACL(filepath.Join(dir.Name(), temporary), info); err != nil {
		return err
	}
	if _, err = f.Write(raw); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	dirInfo, err := dir.Stat(".")
	if err != nil {
		return err
	}
	if err = privateDirectory(dirInfo); err != nil {
		return err
	}
	if err = checkPrivateACL(dir.Name(), dirInfo); err != nil {
		return err
	}
	if expected == nil {
		err = renamex.NoReplace(dir, temporary, name)
	} else {
		current, e := dir.Lstat(name)
		if e != nil {
			return e
		}
		if !os.SameFile(expected, current) {
			return fmt.Errorf("project changed before publication")
		}
		if err = privateRegular(current); err != nil {
			return err
		}
		if err = checkPrivateACL(filepath.Join(dir.Name(), name), current); err != nil {
			return err
		}
		err = dir.Rename(temporary, name)
	}
	if err != nil {
		return err
	}
	published, err := dir.Lstat(name)
	if err != nil {
		return err
	}
	if !os.SameFile(info, published) {
		return fmt.Errorf("project changed during publication")
	}
	if err = privateRegular(published); err != nil {
		return err
	}
	if err = checkPrivateACL(filepath.Join(dir.Name(), name), published); err != nil {
		return err
	}
	return syncProjectDirectory(dir)
}

var setupFields = []string{"version", "source_root", "formatter_bundle", "iso_path", "go_binary"}
var recordFields = []string{"version", "domain", "name", "base", "volume_id", "filesystem_uuid", "size_bytes", "session_id", "backend_object", "initialized", "import_id", "import_source", "imported"}

// Version 1 keeps its original exact field set. Version 2 requires every
// additional field rather than treating absent JSON as an empty binding. The
// initial version read only selects a schema; decodeDocument still rejects
// duplicate versions, case aliases, nulls, unknown fields and trailing objects.
func decodeVersionedDocument(raw []byte, value any, legacy, additions []string) error {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	fields := legacy
	switch header.Version {
	case 1:
	case 2:
		fields = append(append([]string(nil), legacy...), additions...)
	default:
		return fmt.Errorf("unsupported project document version %d", header.Version)
	}
	return decodeDocument(raw, value, fields)
}
func decodeSetup(raw []byte, value *Setup) error {
	return decodeVersionedDocument(raw, value, setupFields, []string{"openssl_path", "openssl_sha256", "xorriso_path", "xorriso_sha256"})
}
func decodeRecord(raw []byte, value *Record) error {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return err
	}
	if header.Version == 3 {
		fields := append(append([]string(nil), recordFields...), "recipe_intent_digest", "import_selection")
		return decodeDocument(raw, value, fields)
	}
	return decodeVersionedDocument(raw, value, recordFields, []string{"recipe_intent_digest"})
}

// All fields are required, including false/empty receipt markers. Tokenizing
// first prevents encoding/json from silently accepting duplicate fields or
// case-insensitive aliases. Scalar nulls are also rejected rather than zeroed.
func decodeDocument(raw []byte, value any, fields []string) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("expected project JSON object")
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid JSON field")
		}
		if seen[name] {
			return fmt.Errorf("duplicate project field %q", name)
		}
		allowed := false
		for _, field := range fields {
			if name == field {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("unknown project field %q", name)
		}
		seen[name] = true
		var scalar json.RawMessage
		if err := decoder.Decode(&scalar); err != nil {
			return err
		}
		if bytes.Equal(bytes.TrimSpace(scalar), []byte("null")) {
			return fmt.Errorf("null project field %q", name)
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("trailing project JSON: %v", err)
	}
	for _, field := range fields {
		if !seen[field] {
			return fmt.Errorf("missing project field %q", field)
		}
	}
	return json.Unmarshal(raw, value)
}
func canonicalPath(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && len(path) <= 4096 && !strings.ContainsAny(path, "\x00\r\n")
}
func validateSetup(s Setup) error {
	if (s.Version != 1 && s.Version != 2) || !canonicalPath(s.SourceRoot) || !canonicalPath(s.FormatterBundle) || !canonicalPath(s.ISOPath) || !canonicalPath(s.GoBinary) || filepath.Base(s.GoBinary) != "go" {
		return fmt.Errorf("invalid project setup version or canonical paths")
	}
	if s.Version == 1 {
		if s.OpenSSLPath != "" || s.OpenSSLSHA256 != "" || s.XorrisoPath != "" || s.XorrisoSHA256 != "" {
			return fmt.Errorf("legacy project setup cannot contain recipe tools")
		}
	} else if !canonicalPath(s.OpenSSLPath) || filepath.Base(s.OpenSSLPath) != "openssl" || !lowerSHA256(s.OpenSSLSHA256) ||
		!canonicalPath(s.XorrisoPath) || filepath.Base(s.XorrisoPath) != "xorriso" || !lowerSHA256(s.XorrisoSHA256) {
		return fmt.Errorf("recipe setup requires exact canonical tool paths and lowercase SHA-256 digests")
	}
	return nil
}

func lowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
