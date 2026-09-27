package alphaprep

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/weshofmann/boxwarden/internal/basebuild"
	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/recipe"
)

// NewRequest derives private, domain-owned attempt paths and fresh build IDs.
// Preparation still revalidates the admitted domain and every source byte.
func NewRequest(selected config.Domain, value recipe.Recipe, isoPath, guestDefinitionRoot string) (basebuild.PrepareRequest, error) {
	for _, path := range []string{selected.StateRoot, isoPath, guestDefinitionRoot} {
		if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\r\n\x00") {
			return basebuild.PrepareRequest{}, errors.New("alpha preparation requires canonical absolute paths")
		}
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return basebuild.PrepareRequest{}, fmt.Errorf("generate fresh preparation identity: %w", err)
	}
	suffix := hex.EncodeToString(nonce[:])
	return basebuild.PrepareRequest{StateRoot: selected.StateRoot, Inputs: basebuild.Inputs{
		AttemptRoot: filepath.Join(selected.StateRoot, "prepared-attempts"),
		AttemptID:   "alpha-attempt-" + suffix,
		CandidateID: "boxwarden-alpha-base-" + suffix,
		RunID:       "run-" + suffix[:12],
		ISOPath:     isoPath, GuestDefinitionRoot: guestDefinitionRoot,
		Recipe: value,
	}}, nil
}
