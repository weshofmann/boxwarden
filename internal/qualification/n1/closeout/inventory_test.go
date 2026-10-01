package closeout

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type noACL struct{}

func (noACL) HasExtendedACL(string) (bool, error) { return false, nil }
func inventoryFixture(t *testing.T) (scope, contract.Handoff) {
	t.Helper()
	r, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	os.Chmod(r, 0700)
	h := contract.Handoff{}
	h.Window.ID = "12345678-1234-1234-1234-123456789abc"
	h.Pair[0].Session = "11111111-1111-1111-1111-111111111111"
	h.Pair[1].Session = "22222222-2222-2222-2222-222222222222"
	h.Pair[0].Backend = "test-control"
	h.Pair[1].Backend = "test-candidate"
	for _, d := range []string{"identity/ssh-user-ca", "identity/ssh-host-pins", "goldens/records", "sessions", "runtime/n1qualification/" + h.Pair[0].Session, "runtime/n1qualification/" + h.Pair[1].Session, "locks"} {
		os.MkdirAll(filepath.Join(r, d), 0700)
	}
	for _, n := range []string{"ca", "ca.pub"} {
		writeFixture(t, filepath.Join(r, "identity/ssh-user-ca", n), []byte("NEVER READ PRIVATE CA"), 0600)
	}
	os.Chmod(filepath.Join(r, "identity/ssh-user-ca/ca.pub"), 0644)
	wire := make([]byte, 51)
	binary.BigEndian.PutUint32(wire[:4], 11)
	copy(wire[4:], "ssh-ed25519")
	binary.BigEndian.PutUint32(wire[15:19], 32)
	for i := 19; i < len(wire); i++ {
		wire[i] = byte(i)
	}
	sum := sha256.Sum256(wire)
	pub := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire)
	writeFixture(t, filepath.Join(r, "identity/ssh-user-ca/ca.pub"), []byte(pub+" boxwarden:n1qualification:management-ca\n"), 0644)
	meta, _ := json.Marshal(struct {
		Version      int    `json:"version"`
		Domain       string `json:"domain"`
		Algorithm    string `json:"algorithm"`
		PublicKey    string `json:"public_key"`
		PublicDigest string `json:"public_digest"`
		Fingerprint  string `json:"fingerprint"`
		CreationUUID string `json:"creation_uuid"`
		CreatorUID   int    `json:"creator_uid"`
		CreatorName  string `json:"creator_name"`
	}{1, "n1qualification", "ssh-ed25519", pub, hex.EncodeToString(sum[:]), "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:]), "44444444-4444-4444-4444-444444444444", 501, "devel"})
	meta = append(meta, '\n')
	for i := range h.Pair {
		p := &h.Pair[i]
		pinRaw, _ := json.Marshal(struct {
			Version       int    `json:"version"`
			Domain        string `json:"domain"`
			SessionID     string `json:"session_id"`
			BackendKind   string `json:"backend_kind"`
			BackendObject string `json:"backend_object"`
			Algorithm     string `json:"algorithm"`
			PublicKey     string `json:"public_key"`
			Fingerprint   string `json:"fingerprint"`
		}{1, "n1qualification", p.Session, "tart", p.Backend, "ssh-ed25519", pub, "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])})
		pinRaw = append(pinRaw, '\n')
		os.WriteFile(filepath.Join(r, "identity/ssh-host-pins/"+p.Session+".json"), pinRaw, 0600)
		p.HostPinSHA = contract.SHA(pinRaw)
	}
	base := []byte(`{"version":1,"domain":"n1qualification","revision":"` + contract.BaseName + `","backend":{"kind":"tart","object_id":"` + contract.BaseName + `"}}`)
	for i, raw := range [][]byte{meta, base} {
		n := contract.PublicRecordNames[i]
		os.WriteFile(filepath.Join(r, n), raw, 0600)
		h.PublicRecords[i] = contract.PublicRecord{Name: n, SHA: contract.SHA(raw), WindowID: h.Window.ID}
	}
	return scope{root: r, uid: os.Getuid(), acl: noACL{}}, h
}
func TestInventoryExhaustiveRefusesResidueWithoutMutation(t *testing.T) {
	s, h := inventoryFixture(t)
	g, e := inventory(context.Background(), s, h)
	if e != nil {
		t.Fatal(e)
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"sessions/.x.tmp-a", "runtime/n1qualification/" + h.Pair[0].Session + "/.gen.stage-a", "runtime/n1qualification/" + h.Pair[0].Session + "/request.json", "quarantine", ".cleanup", "locks/unknown.lock", "volumes/disk.raw"} {
		t.Run(name, func(t *testing.T) {
			s, h := inventoryFixture(t)
			path := filepath.Join(s.root, name)
			os.MkdirAll(filepath.Dir(path), 0700)
			os.WriteFile(path, []byte("residue"), 0600)
			if g, e := inventory(t.Context(), s, h); e == nil {
				g.Close()
				t.Fatal("residue accepted")
			}
			b, e := os.ReadFile(path)
			if e != nil || string(b) != "residue" {
				t.Fatal("inventory mutated residue")
			}
		})
	}
	_ = json.Valid
}
func TestInventoryDeniedUnknownLinkAndLimit(t *testing.T) {
	for _, which := range []string{"link", "foreign", "limit", "read"} {
		t.Run(which, func(t *testing.T) {
			s, h := inventoryFixture(t)
			if which == "link" {
				os.Remove(filepath.Join(s.root, "sessions"))
				os.Symlink(filepath.Join(s.root, "identity"), filepath.Join(s.root, "sessions"))
			}
			if which == "foreign" {
				h.PublicRecords[0].WindowID = "foreign"
			}
			if which == "limit" {
				s.cap = 1
			}
			if which == "read" {
				s.readDir = func(*os.File, int) ([]string, error) { return nil, os.ErrPermission }
			}
			if g, e := inventory(t.Context(), s, h); e == nil {
				g.Close()
				t.Fatal("uncertain inventory accepted")
			}
		})
	}
}

func TestInventoryStrictPublicVersionsAndCurrentRoot(t *testing.T) {
	for _, which := range []string{"ca-version", "ca-unknown", "base-version", "public-mismatch", "renamed-root"} {
		t.Run(which, func(t *testing.T) {
			s, h := inventoryFixture(t)
			if which == "renamed-root" {
				g, e := inventory(t.Context(), s, h)
				if e != nil {
					t.Fatal(e)
				}
				if os.Rename(s.root, s.root+"-old") != nil {
					t.Fatal("rename fixture")
				}
				os.Mkdir(s.root, 0700)
				if g.Revalidate(t.Context()) == nil {
					t.Fatal("detached state namespace admitted")
				}
				g.Close()
				return
			}
			i := 0
			if which == "base-version" {
				i = 1
			}
			path := filepath.Join(s.root, contract.PublicRecordNames[i])
			raw, _ := os.ReadFile(path)
			switch which {
			case "ca-version", "base-version":
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1))
			case "ca-unknown":
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":1,"unknown":true`, 1))
			case "public-mismatch":
				os.WriteFile(filepath.Join(s.root, "identity/ssh-user-ca/ca.pub"), []byte("ssh-ed25519 invalid\n"), 0644)
			}
			os.WriteFile(path, raw, 0600)
			h.PublicRecords[i].SHA = contract.SHA(raw)
			if g, e := inventory(t.Context(), s, h); e == nil {
				g.Close()
				t.Fatal("unadmitted public identity/schema accepted")
			}
		})
	}
}

func TestRetainedOriginalPinsOnlyAndNoPrivateReads(t *testing.T) {
	for _, kind := range []string{"missing", "foreign", "schema", "fingerprint", "changed", "extra"} {
		t.Run(kind, func(t *testing.T) {
			s, h := inventoryFixture(t)
			path := filepath.Join(s.root, "identity/ssh-host-pins/"+h.Pair[0].Session+".json")
			raw, _ := os.ReadFile(path)
			switch kind {
			case "missing":
				os.Remove(path)
			case "foreign":
				raw = []byte(strings.Replace(string(raw), `"backend_object":"test-control"`, `"backend_object":"foreign"`, 1))
				h.Pair[0].HostPinSHA = contract.SHA(raw)
				os.WriteFile(path, raw, 0600)
			case "schema":
				raw = []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1))
				h.Pair[0].HostPinSHA = contract.SHA(raw)
				os.WriteFile(path, raw, 0600)
			case "fingerprint":
				raw = []byte(strings.Replace(string(raw), `SHA256:`, `INVALID:`, 1))
				h.Pair[0].HostPinSHA = contract.SHA(raw)
				os.WriteFile(path, raw, 0600)
			case "changed":
				os.WriteFile(path, append(raw, ' '), 0600)
			case "extra":
				os.WriteFile(filepath.Join(s.root, "identity/ssh-host-pins/33333333-3333-3333-3333-333333333333.json"), raw, 0600)
			}
			if g, e := inventory(t.Context(), s, h); e == nil {
				g.Close()
				t.Fatal("foreign/missing/changed pin accepted")
			}
			private, _ := os.ReadFile(filepath.Join(s.root, "identity/ssh-user-ca/ca"))
			if string(private) != "NEVER READ PRIVATE CA" {
				t.Fatal("private CA modified")
			}
		})
	}
}

func writeFixture(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if e := os.WriteFile(path, raw, mode); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, mode); e != nil {
		t.Fatal(e)
	}
	s, e := os.Lstat(path)
	if e != nil || s.Mode().Perm() != mode {
		t.Fatal("fixture metadata", s, e)
	}
}
