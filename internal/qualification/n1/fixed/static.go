package fixed

import "os"
import "github.com/weshofmann/boxwarden/internal/qualification/n1/contract"

func modeFile(mode uint32) os.FileMode { return os.FileMode(mode) }

type StaticInputs struct {
	Lock      contract.StaticLock
	Catalogue contract.Catalogue
	Protected contract.ProtectedInventory
	Build     contract.BuildInputs
	LockSHA   string
}
type staticReader func(string, int, int, uint32, int) ([]byte, error)

func ReadStatic() (StaticInputs, error) {
	return readStatic(func(p string, cap, uid int, mode uint32, gid int) ([]byte, error) {
		return read(p, cap, uid, modeFile(mode), gid)
	}, func(p string) error { return checkDirectory(p, 501, 0700) })
}
func readStatic(read staticReader, dir func(string) error) (StaticInputs, error) {
	var v StaticInputs
	for _, p := range []string{contract.PackageRoot, contract.ConfigRoot, contract.PackageRoot + "/config-expected", contract.QualificationRoot} {
		if dir(p) != nil {
			return v, ErrRefused
		}
	}
	raw, e := read(contract.LockPath, contract.MaxLockBytes, 501, 0600, -1)
	if e != nil {
		return v, ErrRefused
	}
	v.Lock, e = contract.ParseStaticLock(raw)
	if e != nil {
		return v, ErrRefused
	}
	v.LockSHA = contract.SHA(raw)
	for i, a := range v.Lock.Artifacts {
		raw, e = read(contract.ArtifactPath(i), 128<<20, 501, 0500, -1)
		if e != nil || contract.SHA(raw) != a.SHA {
			return StaticInputs{}, ErrRefused
		}
	}
	for i, f := range v.Lock.Files {
		mode := uint32(0600)
		if i <= 4 || i >= 6 && i <= 9 {
			mode = 0500
		}
		raw, e = read(contract.StaticFilePath(i), 128<<20, 501, mode, -1)
		if e != nil || contract.SHA(raw) != f.SHA {
			return StaticInputs{}, ErrRefused
		}
	}
	records := [][]byte{}
	for i := 0; i <= len(v.Lock.SystemImages); i++ {
		raw, e = read(contract.QualificationPath(i), 8192, 501, 0600, -1)
		if e != nil {
			return StaticInputs{}, ErrRefused
		}
		records = append(records, raw)
	}
	v.Catalogue, e = contract.AdmitCatalogue(v.Lock, records)
	if e != nil {
		return StaticInputs{}, ErrRefused
	}

	raw, e = read(contract.StaticFilePath(contract.ProtectedRecordIndex), contract.MaxLockBytes, 501, 0600, -1)
	if e != nil {
		return StaticInputs{}, ErrRefused
	}
	v.Protected, e = contract.ParseProtectedInventory(raw, v.Lock.Files[contract.ProtectedRecordIndex].SHA)
	if e != nil {
		return StaticInputs{}, ErrRefused
	}
	raw, e = read(contract.StaticFilePath(23), contract.MaxLockBytes, 501, 0600, -1)
	if e != nil {
		return StaticInputs{}, ErrRefused
	}
	v.Build, e = contract.ParseBuildInputs(raw, v.Lock.Files[23].SHA)
	if e != nil {
		return StaticInputs{}, ErrRefused
	}
	return v, nil
}
