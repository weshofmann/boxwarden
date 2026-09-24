package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type AlphaExportInput struct {
	VolumeID, DestinationParent   string
	Selected                      []string
	SourceRoot, ISOPath, GoBinary string
}

type AlphaExportFunc func(context.Context, config.Domain, AlphaExportInput, backend.Observer) (workspacex.ExportJournal, string, error)

type exportSelections []string

func (selected *exportSelections) String() string { return strings.Join(*selected, ",") }
func (selected *exportSelections) Set(path string) error {
	*selected = append(*selected, path)
	return nil
}

func validAlphaExportInput(input AlphaExportInput) error {
	if input.VolumeID == "" || len(input.Selected) == 0 || len(input.Selected) > 1024 {
		return errors.New("workspace export requires a volume and selected paths")
	}
	for _, path := range []string{input.DestinationParent, input.SourceRoot, input.ISOPath, input.GoBinary} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("workspace export paths must be clean and absolute")
		}
	}
	if filepath.Base(input.GoBinary) != "go" {
		return errors.New("workspace export requires an exact Go executable path")
	}
	for _, path := range input.Selected {
		if path == "" || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("workspace export selection is empty or malformed")
		}
	}
	return nil
}

func writeAlphaExport(output io.Writer, selected config.Domain, journal workspacex.ExportJournal, published string) error {
	if journal.ID == "" || journal.Domain != selected.ID || journal.Phase != workspacex.ExportPublished ||
		published != filepath.Join(journal.DestinationParent, strings.ReplaceAll(journal.ID, "-", "")) {
		return errors.New("workspace export returned an invalid publication receipt")
	}
	_, err := fmt.Fprintf(output, "domain: %s\ntransaction: %s\nexport: %s\n", selected.ID, journal.ID, published)
	return err
}
