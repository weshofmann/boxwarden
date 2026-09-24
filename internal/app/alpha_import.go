package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type AlphaImportInput struct {
	TransactionID, SourcePath, VolumeID, SessionName string
	Resume                                           bool
}

type AlphaImportFunc func(context.Context, config.Domain, AlphaImportInput) (workspacex.ImportJournal, supervisor.ImportResult, error)

func validAlphaImportInput(input AlphaImportInput) error {
	if !alphaCreateUUID(input.VolumeID) || input.SessionName == "" {
		return errors.New("workspace import requires one canonical volume UUID and session name")
	}
	if _, err := session.ParseName(input.SessionName); err != nil {
		return err
	}
	if input.Resume {
		if !alphaCreateUUID(input.TransactionID) || input.SourcePath != "" {
			return errors.New("workspace import resume requires one canonical transaction UUID and no source path")
		}
	} else if input.SourcePath == "" || !filepath.IsAbs(input.SourcePath) || filepath.Clean(input.SourcePath) != input.SourcePath || strings.ContainsAny(input.SourcePath, "\r\n\x00") {
		return errors.New("workspace import requires a clean absolute --source path")
	}
	return nil
}

func writeAlphaImport(output io.Writer, selected config.Domain, input AlphaImportInput, journal workspacex.ImportJournal, receipt supervisor.ImportResult) error {
	if journal.ID != input.TransactionID || journal.Domain != selected.ID || journal.VolumeID != input.VolumeID || journal.SessionName != input.SessionName ||
		journal.Phase != workspacex.ImportTransferring || receipt.Digest != journal.SourceDigest || receipt.FileCount != journal.FileCount ||
		receipt.TotalBytes != journal.TotalBytes || receipt.RemotePath != journal.MountPath+"/boxwarden-import-"+journal.ID {
		return errors.New("workspace import returned an invalid transfer receipt")
	}
	_, err := fmt.Fprintf(output, "domain: %s\nvolume: %s\nimport: readback-matched\njournal: transferring\nremote: %s\n", selected.ID, input.VolumeID, receipt.RemotePath)
	return err
}
