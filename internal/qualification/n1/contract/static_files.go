package contract

// Closed package file table. Paths are compile-pinned, never command authority.
// Source snapshots/build and tool records bind producing commits and tool
// inputs separately from final artifacts to avoid self-hash cycles.
var StaticNames = [...]string{
	"artifacts/boxwarden-stock", "artifacts/boxwarden-candidate", "artifacts/n1-stock-worker", "artifacts/n1-candidate-worker", "artifacts/softnet-diagnostic", "artifacts/softnet-diagnostic.tar", "artifacts/guest-bootstrap-trial", "artifacts/clipboard-overlay", "artifacts/clipboard-adapter-production", "artifacts/guest-metadata-observer.py",
	"config/stock.source.json", "config/candidate.source.json", "config/postcloseout-doctor.json", "config-expected/stock.enrolled.json", "config-expected/candidate.enrolled.json",
	"sources/source-snapshot.json", "sources/stager.py", "sources/inspector.py", "sources/controls.py", "sources/connect.py", "sources/followup_tcp_verdict.py", "sources/clipboard-adjudicator.go", "sources/network-adjudicator.go",
	"records/build-inputs.json", "records/go-twin-proof.json", "records/rust-twin-proof.json", "records/base-admission.json", "records/stock-manifest.json", "procedures/owner-window.txt", "procedures/runtime-review.txt", "procedures/command-catalogue.json", "procedures/runtime-schema.json", "records/protected-inventory.json", "artifacts/guest-bootstrap-generic",
}

const GenericBootstrapSHA = "33b12b9e293bcbdf7d6c91f933b42003ca394e7a678ac83b8d80df714d27c57e"

type StaticFile struct {
	Name   string `json:"name"`
	SHA    string `json:"sha"`
	Source string `json:"source"`
}

func validStaticFiles(xs []StaticFile) bool {
	if len(xs) != len(StaticNames) {
		return false
	}
	for i, x := range xs {
		if x.Name != StaticNames[i] || !digest(x.SHA) || !lowerHex(x.Source, 40) {
			return false
		}
	}
	return xs[13].SHA == StockConfigSHA && xs[14].SHA == CandidateConfigSHA && xs[4].SHA == SoftnetSHA && xs[33].SHA == GenericBootstrapSHA
}
func StaticFilePath(index int) string {
	if index < 0 || index >= len(StaticNames) {
		return ""
	}
	return PackageRoot + "/" + StaticNames[index]
}

func EnrolledTarget(index int) string {
	if index < 0 || index > 1 {
		return ""
	}
	return ConfigRoot + "/" + []string{"stock.enrolled.json", "candidate.enrolled.json"}[index]
}
