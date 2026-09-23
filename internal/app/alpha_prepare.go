package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/backend"
	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
)

// AlphaPrepareInput selects local source and exact-digest auxiliary tools.
// The host toolchain and selected domain are admitted again by alphaprep.
type AlphaPrepareInput struct {
	RecipePath, ISOPath, GuestDefinitionRoot string
	OpenSSLPath, OpenSSLSHA256               string
	XorrisoPath, XorrisoSHA256               string
}

type AlphaPrepareFunc func(context.Context, config.Config, config.Domain, string, AlphaPrepareInput) (basebuild.PreparedResult, error)

func validAlphaPrepareInput(input AlphaPrepareInput) error {
	for _, path := range []string{input.RecipePath, input.ISOPath, input.GuestDefinitionRoot, input.OpenSSLPath, input.XorrisoPath} {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\r\n\x00") {
			return errors.New("alpha prepare paths must be canonical and absolute")
		}
	}
	if filepath.Base(input.OpenSSLPath) != "openssl" || filepath.Base(input.XorrisoPath) != "xorriso" || !lowerSHA(input.OpenSSLSHA256) || !lowerSHA(input.XorrisoSHA256) {
		return errors.New("alpha prepare tools require exact paths and lowercase SHA-256 digests")
	}
	return nil
}

func lowerSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func writeAlphaPrepared(output io.Writer, selected config.Domain, result basebuild.PreparedResult) error {
	record := result.Record
	qualification := record.Qualification
	attemptRoot := filepath.Join(selected.StateRoot, "prepared-attempts")
	if result.Disposition != basebuild.PreparedBuilt && result.Disposition != basebuild.PreparedReused || record.Version != 2 ||
		backend.ValidateObjectID(record.CandidateID) != nil || backend.ValidateObjectID(qualification.CloneID) != nil ||
		record.PreparationKey != qualification.PreparationKey || record.CandidateID != qualification.CandidateID || !qualification.Passed ||
		!lowerSHA(record.PreparationKey) || !lowerSHA(record.CandidateIdentity) || !lowerSHA(qualification.EvidenceSHA256) || !lowerSHA(qualification.BOMSHA256) ||
		filepath.Dir(record.AttemptDirectory) != attemptRoot {
		return errors.New("alpha preparation returned an invalid cache receipt")
	}
	_, err := fmt.Fprintf(output, "domain: %s\nprepared-base: %s\npreparation-key: %s\ncache: %s\n", selected.ID, record.CandidateID, record.PreparationKey, result.Disposition)
	return err
}
