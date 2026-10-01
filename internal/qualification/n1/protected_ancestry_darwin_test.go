//go:build darwin && n1diagnostic && n1cleanup && !n1candidate

package n1

import (
	"encoding/json"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"os"
	"testing"
)

func actualProtectedAncestry(t *testing.T) contract.ProtectedInventory {
	t.Helper()
	raw, e := os.ReadFile("contract/testdata/protected-ancestry.json")
	if e != nil {
		t.Fatal(e)
	}
	if contract.SHA(raw) != "ccb9ea8a575605aa53e9b8727bfa9d88bf475f116c8bbe8fde25006352fb4652" {
		t.Fatal("actual proof changed")
	}
	var proof struct{ Records []contract.ImageMetadata }
	if json.Unmarshal(raw, &proof) != nil {
		t.Fatal("proof parse")
	}
	r := protectedFixture()
	for j := range r.Directories {
		for _, x := range proof.Records {
			if r.Directories[j].Metadata.Path == x.Path {
				r.Directories[j].Metadata = x
			}
		}
	}
	r.Device = 16777245
	for j := range r.Objects {
		for k := range r.Objects[j].Files {
			r.Objects[j].Files[k].Metadata.Device = r.Device
		}
	}
	r.Directories[len(r.Directories)-1].Metadata.Device = r.Device
	return r
}
func TestActualProtectedAncestryAdmitted(t *testing.T) {
	r := actualProtectedAncestry(t)
	if !r.Valid() {
		t.Fatal("actual qualified home0750/single-deny-delete ancestry refused")
	}
}

func TestProtectedDirectoryACLRecordCannotBroaden(t *testing.T) {
	r := actualProtectedAncestry(t)
	for j, d := range r.Directories {
		if contract.ProtectedAncestorMode(d.Metadata.Path) == 0 {
			continue
		}
		for _, change := range []func(*contract.ProtectedDirectory){func(d *contract.ProtectedDirectory) { d.Metadata.NoACL = true }, func(d *contract.ProtectedDirectory) { d.ACLKind = contract.NoExtendedACL }, func(d *contract.ProtectedDirectory) { d.ACLKind = "allow-write" }, func(d *contract.ProtectedDirectory) { d.Metadata.UID = 0 }, func(d *contract.ProtectedDirectory) { d.Metadata.GID = 0 }, func(d *contract.ProtectedDirectory) { d.Metadata.Mode = 0755 }, func(d *contract.ProtectedDirectory) { d.Metadata.Path += "/foreign" }} {
			x := r.Directories[j]
			change(&x)
			if x.Valid() {
				t.Fatal("broadened directory admitted", x)
			}
		}
	}
	raw, _ := json.Marshal(r)
	if _, e := contract.ParseProtectedInventory(raw, contract.SHA(raw)); e != nil {
		t.Fatal(e)
	}
}
