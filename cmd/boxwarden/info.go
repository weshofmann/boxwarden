package main

import (
	"fmt"
	"io"
)

// Set by the private-beta build. Development builds remain identifiable.
var buildVersion = "development"
var buildRevision = "unknown"

// Information commands do not load configuration or construct host adapters.
func runInfo(args []string, out io.Writer) (bool, error) {
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
boxwarden --config /absolute/config.json doctor
boxwarden --config /absolute/config.json --domain alpha project COMMAND

project list
project setup --source-root PATH --formatter-bundle PATH --iso PATH --go PATH
project create [--base current|REGISTERED-BASE] [--size-mib 16..1024] NAME
project open|status|stop NAME
project import --source PRIVATE-DIRECTORY NAME
project import retry NAME
project export --destination NEW-DIRECTORY NAME

clipboard targets                       explicit --domain required
clipboard copy NAME                     synthetic/text stdin → guest
clipboard paste NAME                    guest → redirected stdout
clipboard push|pull NAME                explicit general Mac clipboard transfer

See the archive's TRY-ME.md for one-time setup and the stopped export workflow.
Host installation and an admitted prepared Ubuntu Desktop base are prerequisites.`)
		return true, err
	default:
		return false, nil
	}
}
