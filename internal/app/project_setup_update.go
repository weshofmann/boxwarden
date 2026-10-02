package app

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/weshofmann/boxwarden/internal/projectx"
)

func updateProjectSetup(stateRoot string, next projectx.Setup, out io.Writer) error {
	history, err := projectx.UpdateSetup(stateRoot, next)
	if err != nil {
		return fmt.Errorf("project setup-update incomplete; retain both packages and rerun the identical setup-update command to finish publication/sync; do not delete either asset set; archived previous setup: %q: %w", history, err)
	}
	if history == "" {
		_, err = fmt.Fprintln(out, "project setup-update: unchanged; no workspace or guest helper was changed")
	} else {
		_, err = fmt.Fprintf(out, "project setup-update: saved; no workspace or guest helper was changed\nprevious setup: %s\nnew setup: %s\n", history, filepath.Join(stateRoot, "projects", ".setup.json"))
	}
	return err
}
