package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/workspacex"
)

type AlphaWorkspaceCreateInput struct {
	VolumeID, FilesystemUUID string
	SizeBytes                int64
	BundlePath, SourceRoot   string
}

type AlphaWorkspaceCreateFunc func(context.Context, config.Domain, AlphaWorkspaceCreateInput) (workspacex.Record, error)

func validAlphaWorkspaceCreateInput(input AlphaWorkspaceCreateInput) error {
	if !alphaCreateUUID(input.VolumeID) || !alphaCreateUUID(input.FilesystemUUID) || input.SizeBytes < 16<<20 || input.SizeBytes > 1<<43 || input.SizeBytes%(1<<20) != 0 {
		return errors.New("workspace create requires canonical volume/filesystem UUIDs and a size from 16 MiB to 8 TiB")
	}
	for _, path := range []string{input.BundlePath, input.SourceRoot} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("workspace create paths must be clean and absolute")
		}
	}
	return nil
}

func alphaCreateUUID(raw string) bool {
	if len(raw) != 36 {
		return false
	}
	for i := range raw {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if raw[i] != '-' {
				return false
			}
		} else if raw[i] < '0' || raw[i] > '9' && raw[i] < 'a' || raw[i] > 'f' {
			return false
		}
	}
	return true
}
