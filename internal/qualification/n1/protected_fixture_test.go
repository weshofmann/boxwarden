package n1

import (
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"path"
)

func protectedFixture() contract.ProtectedInventory {
	r := contract.ProtectedInventory{Version: 1, Root: contract.TartRoot, VolumeUUID: contract.TartVolumeUUID, Device: 7, RootChildren: []string{"vms"}}
	meta := func(p string, dir bool) contract.ImageMetadata {
		x := contract.ImageMetadata{Path: p, Device: 7, Inode: 13, UID: 501, GID: 20, Mode: 0600, Nlink: 1, Bytes: 1, MtimeNS: 1, CtimeNS: 1, NoACL: true}
		if dir {
			x.Mode = 0700
			x.Nlink = 2
		}
		return x
	}
	for p := r.Root; ; p = path.Dir(p) {
		x := meta(p, true)
		kind := contract.NoExtendedACL
		if mode := contract.ProtectedAncestorMode(p); mode != 0 {
			x.Mode = mode
			x.NoACL = false
			kind = contract.EveryoneDenyDelete
		}
		r.Directories = append([]contract.ProtectedDirectory{{Metadata: x, ACLKind: kind}}, r.Directories...)
		if p == "/" {
			break
		}
	}
	r.Directories = append(r.Directories, contract.ProtectedDirectory{Metadata: meta(r.Root+"/vms", true), ACLKind: contract.NoExtendedACL}, contract.ProtectedDirectory{Metadata: meta(r.Root+"/vms/"+contract.BaseName, true), ACLKind: contract.NoExtendedACL})
	o := contract.ProtectedObject{Name: contract.BaseName}
	for j, n := range []string{"config.json", "nvram.bin", "disk.img"} {
		o.Files[j].Metadata = meta(r.Root+"/vms/"+o.Name+"/"+n, false)
		if j < 2 {
			o.Files[j].SHA = contract.SHA([]byte("x"))
		} else {
			o.Files[j].Metadata.Bytes = 1 << 40
		}
	}
	r.Objects = []contract.ProtectedObject{o}
	return r
}
