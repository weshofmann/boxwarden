package sessionruntime

import (
	"fmt"
	"slices"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/session"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// admitOwnerAction independently rechecks the caller's exact durable host
// intent before the retained owner can use its generation SSH credentials.
// The caller holds the session transition lock; no guest command is run here.
func admitOwnerAction(stateRoot, sessionName string, binding supervisor.Binding, request guestproto.ActionRequest) error {
	if stateRoot == "" || sessionName == "" {
		return fmt.Errorf("incomplete action owner binding")
	}
	if _, _, err := guestproto.EncodeActionRequest(request); err != nil {
		return err
	}
	if request.ActionPhase == "launch" || request.Association != (guestproto.Association{
		Domain: binding.Domain, SessionID: binding.SessionID,
		BackendKind: binding.BackendKind, BackendObject: binding.BackendObject,
	}) || request.Generation != binding.Generation {
		return fmt.Errorf("action request differs from retained generation")
	}
	domainID, err := domain.Parse(binding.Domain)
	if err != nil {
		return err
	}
	record, err := session.LoadRecord(stateRoot, binding.Domain, sessionName)
	if err != nil {
		return err
	}
	if record.Domain != domainID || string(record.Name) != sessionName || record.ID != binding.SessionID ||
		record.Backend.Kind != binding.BackendKind || record.Backend.ObjectID != binding.BackendObject ||
		record.StartGeneration != binding.Generation || record.RecipeIntentDigest != request.RecipeDigest ||
		record.IntendedState != session.StateRunning || record.Readiness.Status != session.ReadinessReady {
		return fmt.Errorf("durable session changed before guest action")
	}
	if err := session.RequireNoRebuild(stateRoot, domainID, sessionName); err != nil {
		return err
	}
	attempt, err := session.LoadActionAttempt(stateRoot, domainID, binding.SessionID, request.AttemptID)
	if err != nil {
		return fmt.Errorf("load exact reserved action attempt: %w", err)
	}
	if attempt.State != session.ActionAttemptReserved || attempt.SessionName != sessionName ||
		attempt.BackendObject != binding.BackendObject || attempt.Generation != binding.Generation ||
		attempt.RecipeDigest != request.RecipeDigest || attempt.ActionID != request.ActionID ||
		attempt.ActionPhase != request.ActionPhase {
		return fmt.Errorf("action attempt is not the exact reserved intent")
	}
	raw, err := session.LoadRecipeIntent(stateRoot, request.RecipeDigest)
	if err != nil {
		return err
	}
	intent, err := recipe.DecodeIntent(raw)
	if err != nil {
		return err
	}
	for _, step := range intent.Steps {
		if step.ID == request.ActionID && step.Phase == request.ActionPhase && slices.Equal(step.Argv, request.Argv) {
			return nil
		}
	}
	return fmt.Errorf("action argv differs from exact stored recipe step")
}
