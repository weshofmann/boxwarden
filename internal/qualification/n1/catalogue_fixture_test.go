package n1

import (
	"encoding/json"
	"fmt"
	"github.com/weshofmann/boxwarden/internal/qualification/n1/contract"
	"path"
	"path/filepath"
	"strings"
)

func censusFixture() (contract.StaticLock, contract.Catalogue) {
	s := contract.StaticLock{Version: 2, Configs: [2]string{contract.StockConfigSHA, contract.CandidateConfigSHA}, SoftnetSHA: contract.SoftnetSHA, CatalogueSHA: strings.Repeat("a", 64), SchemaSHA: strings.Repeat("b", 64), ProcedureSHA: strings.Repeat("c", 64), NoReplacement: true, ActiveSeconds: 1200, AttendanceSeconds: 600}
	for i := range s.Artifacts {
		s.Artifacts[i] = contract.Artifact{Name: filepath.Base(contract.ArtifactPath(i)), SHA: fmt.Sprintf("%064x", i+1), Source: strings.Repeat("a", 40)}
	}
	for i, n := range contract.StaticNames {
		s.Files = append(s.Files, contract.StaticFile{Name: n, SHA: fmt.Sprintf("%064x", i+100), Source: strings.Repeat("b", 40)})
	}
	s.Files[4].SHA = contract.SoftnetSHA
	s.Files[13].SHA = contract.StockConfigSHA
	s.Files[14].SHA = contract.CandidateConfigSHA
	s.Files[30].SHA = s.CatalogueSHA
	s.Files[31].SHA = s.SchemaSHA
	s.Files[28].SHA = s.ProcedureSHA
	meta := func(p string, dev, ino, links, size, ts uint64, mode, flags uint32) contract.ImageMetadata {
		return contract.ImageMetadata{Path: p, Device: dev, Inode: ino, UID: 0, GID: 0, Mode: mode, Nlink: links, Bytes: size, MtimeNS: ts, CtimeNS: ts, Flags: flags, NoACL: true}
	}
	q := contract.Qualification{Version: 1, Kind: "protected-sudo", Path: contract.SudoPath, Platform: contract.Platform{OS: "darwin", Arch: "arm64", Release: "27.0.1", Build: "26A434"}, SHA: "", IntendedUse: "fixed native privilege status relay", Leaf: meta(contract.SudoPath, 16777230, 1152921500312608819, 1, 2362384, 1790233837000000000, 04511, 524320)}
	q.Ancestors = []contract.ImageMetadata{meta("/", 16777230, 2, 23, 736, 1790233837000000000, 0755, 1048576), meta("/usr", 16777230, 1152921500312607501, 11, 352, 1790233837000000000, 0755, 557056), meta("/usr/bin", 16777230, 1152921500312607504, 934, 29888, 1790233837000000000, 0755, 524288)}
	raw, _ := json.Marshal(q)
	records := [][]byte{raw}
	s.ProtectedSudo = contract.ProtectedSudo{Kind: "protected-sudo", Path: contract.SudoPath, QualificationSHA: contract.SHA(raw)}
	for i, p := range contract.SystemPaths {
		q := contract.Qualification{Version: 1, Kind: "digest", Path: p, Platform: contract.Platform{OS: "darwin", Arch: "arm64", Release: "27.0.1", Build: "26A434"}, SHA: fmt.Sprintf("%064x", i+40), IntendedUse: "synthetic approved exact OS path", Leaf: meta(p, 1, uint64(i+100), 1, 1, 1, 0755, 0)}
		for parent := path.Dir(p); ; parent = path.Dir(parent) {
			q.Ancestors = append([]contract.ImageMetadata{meta(parent, 1, 1, 2, 1, 1, 0755, 0)}, q.Ancestors...)
			if parent == "/" {
				break
			}
		}
		raw, _ := json.Marshal(q)
		records = append(records, raw)
		s.SystemImages = append(s.SystemImages, contract.SystemImage{Kind: "digest", Path: p, SHA: q.SHA, QualificationSHA: contract.SHA(raw)})
	}
	c, e := contract.AdmitCatalogue(s, records)
	if e != nil {
		panic(e)
	}
	return s, c
}
