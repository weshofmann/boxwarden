package app

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/projectx"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

// Discovery uses bookmarks only as journal locators. No live backend, readiness,
// replacement/session/workspace admission, lock or retry is needed to discover
// retained records. Effects keep their existing independent exact admission.
func listProjectExports(ctx context.Context, d config.Domain, name string, output io.Writer) error {
	r, err := projectx.Load(d.StateRoot, d.ID, name)
	if err != nil {
		return fmt.Errorf("load project %s: %w", name, err)
	}
	journals, err := workspacex.ListExportJournals(ctx, d.StateRoot, d.ID, r.VolumeID)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	fmt.Fprintln(&out, "exports: recorded journal snapshot; phases and paths are not current filesystem or recovery proof")
	if len(journals) == 0 {
		fmt.Fprintln(&out, "no retained export transactions for this project workspace")
	}
	for _, j := range journals {
		if err := ctx.Err(); err != nil {
			return err
		}
		matches := projectExportMatches(j, d, r)
		binding := "matches current bookmark; effects re-admit the live binding"
		if !matches {
			binding = "historical or different selection; inspect before recovery"
		}
		fmt.Fprintf(&out, "transaction: %s\nrecorded phase: %s\nbookmark binding: %s\nrecorded system: %s (session %s; project %s)\ndestination parent: %q\n", j.ID, j.Phase, binding, j.BackendObject, j.SessionID, j.SessionName, j.DestinationParent)
		if j.Phase == workspacex.ExportPublished {
			published := filepath.Join(j.DestinationParent, strings.ReplaceAll(j.ID, "-", ""))
			fmt.Fprintf(&out, "recorded export: %q (inspect; never overwritten)\n", published)
			if r.ImportID != "" && len(j.Selected) == 1 && j.Selected[0] == "boxwarden-import-"+r.ImportID {
				fmt.Fprintf(&out, "recorded project files: %q\n", filepath.Join(published, j.Selected[0]))
			}
		} else if matches {
			fmt.Fprintf(&out, "explicit recovery: project export retry --transaction %s %s (after the command has returned; keep sandbox stopped; admission may still refuse uncertain effects)\n", j.ID, r.Name)
		}
		fmt.Fprintln(&out)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = output.Write(out.Bytes())
	return err
}
