package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/weshofmann/boxwarden/internal/importx"
)

type importExclusions []string

func (v *importExclusions) String() string { return strings.Join(*v, ",") }
func (v *importExclusions) Set(value string) error {
	if len(*v) >= importx.MaxExclusions {
		return errors.New("import accepts at most 32 literal exclusions")
	}
	*v = append(*v, value)
	return nil
}

func previewProjectImport(ctx context.Context, p projectCommand, output io.Writer) error {
	selection, err := importx.ParseSelection(p.selection)
	if err != nil {
		return err
	}
	snapshot, err := importx.PreviewSource(ctx, p.source, selection)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "source: %s\nselected: %d files, %d directories, %d bytes\nselection digest: %s\n", p.source, snapshot.FileCount, snapshot.DirectoryCount, snapshot.TotalBytes, snapshot.Digest); err != nil {
		return err
	}
	for _, excluded := range selection.Excludes {
		if _, err := fmt.Fprintf(output, "excluded: %s\n", excluded); err != nil {
			return err
		}
	}
	for _, entry := range snapshot.Entries {
		if _, err := fmt.Fprintf(output, "%s\t%s\t%d\t%s\n", entry.Kind, entry.Path, entry.Size, entry.SHA256); err != nil {
			return err
		}
	}
	return nil
}
