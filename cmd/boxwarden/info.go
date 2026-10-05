package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/hostx"
)

// Set by the private-beta build. Development builds remain identifiable.
var buildVersion = "development"
var buildRevision = "unknown"

// Information commands do not load configuration or construct host adapters.
func runInfo(args []string, out io.Writer) (bool, error) {
	if len(args) == 2 && args[0] == "build-info" && args[1] == "--json" {
		policy := "stock"
		if hostx.SoftnetBlockTarget != "" {
			policy = "n1candidate"
		}
		// Compiled selection is inspectable without admission or live host claims.
		data := struct {
			Version       string            `json:"version"`
			Revision      string            `json:"revision"`
			NetworkPolicy map[string]string `json:"network_policy"`
		}{buildVersion, buildRevision, map[string]string{
			"build": policy, "description": hostx.NetworkPolicyBuild,
			"softnet_version":           hostx.SoftnetVersion,
			"softnet_executable_sha256": hostx.SoftnetExecutableSHA256,
			"softnet_archive_sha256":    hostx.SoftnetArchiveSHA256,
			"block_target":              hostx.SoftnetBlockTarget,
		}}
		return true, json.NewEncoder(out).Encode(data)
	}
	if len(args) != 1 {
		return false, nil
	}
	switch args[0] {
	case "version", "--version":
		_, err := fmt.Fprintf(out, "Boxwarden %s\nrevision: %s\n", buildVersion, buildRevision)
		return true, err
	case "help", "--help", "-h":
		_, err := fmt.Fprintln(out, `Boxwarden — graphical projects on an initialized Apple Silicon Mac

boxwarden version
boxwarden build-info --json
boxwarden [--config /absolute/config.json] setup inspect --json
boxwarden --config /absolute/config.json setup prepare --json --package ROOT --iso PATH --checker PATH --go PATH --zstd PATH --openssl PATH --xorriso PATH
boxwarden --config /absolute/config.json doctor
boxwarden --config /absolute/config.json --domain alpha project COMMAND

project list
project setup|setup-update --source-root PATH --formatter-bundle PATH --iso PATH --go PATH
project create [--recipe desktop|actions|chatgpt | --base current|REGISTERED-BASE] [--size-mib 16..4096] NAME
project open|status|stop NAME
project rebuild [--recipe desktop|actions|chatgpt | --base current|REGISTERED-BASE] NAME
project rebuild retry NAME
project import preview --source PRIVATE-DIRECTORY [--exclude RELATIVE-PATH ...]
project import --source PRIVATE-DIRECTORY [--exclude RELATIVE-PATH ...] [--expected-digest SHA256] NAME
project import retry NAME
project export list NAME
project export --destination NEW-DIRECTORY NAME
project export retry --transaction UUID NAME

clipboard targets                       explicit --domain required
clipboard copy NAME                     synthetic/text stdin → guest
clipboard paste NAME                    guest → redirected stdout
clipboard push|pull NAME                explicit general Mac clipboard transfer

See the archive's TRY-ME.md for one-time setup and the stopped export workflow.
Host installation and pinned Ubuntu inputs are prerequisites. Recipe preparation
installs new-system software/support and reuses matching qualified preparation.`)
		return true, err
	default:
		return false, nil
	}
}
