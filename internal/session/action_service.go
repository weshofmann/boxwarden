package session

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/guestproto"
	"github.com/weshofmann/boxwarden/internal/lock"
	"github.com/weshofmann/boxwarden/internal/recipe"
	"github.com/weshofmann/boxwarden/internal/supervisor"
)

// ActionControl is the exact-generation supervisor capability. The retained
// owner must independently re-admit the attempt before using its SSH secret.
type ActionControl interface {
	Snapshot(context.Context, supervisor.Binding) (supervisor.Snapshot, error)
	RunAction(context.Context, supervisor.Binding, guestproto.ActionRequest) (guestproto.ActionReceipt, error)
}

// ActionService owns the durable host transition around one guest-only step.
// Public recipe admission stays closed until the retained-owner control route
// and explicit recovery operations are implemented and qualified.
type ActionService struct {
	domain  config.Domain
	control ActionControl
	now     func() time.Time
	newID   func() (string, error)
}

func NewActionService(configured config.Domain, control ActionControl) *ActionService {
	return &ActionService{domain: configured, control: control, now: time.Now, newID: newSessionID}
}

// ExecuteAction runs one explicitly selected stored recipe step. It holds the
// transition and session locks across reservation, execution, and completion
// so a concurrent stop, start, rebuild, or other action cannot change its
// durable binding. An ambiguous guest result becomes non-replayable.
func (s *ActionService) ExecuteAction(ctx context.Context, rawName, phase, actionID string) (result ActionAttempt, err error) {
	if s == nil || s.control == nil || s.now == nil || s.newID == nil {
		return ActionAttempt{}, fmt.Errorf("action service dependencies are required")
	}
	domainID, parseErr := domain.Parse(string(s.domain.ID))
	if parseErr != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return ActionAttempt{}, fmt.Errorf("invalid configured domain")
	}
	name, err := ParseName(rawName)
	if err != nil {
		return ActionAttempt{}, err
	}
	if phase != "once" && phase != "reconfigure" && phase != "startup" {
		return ActionAttempt{}, fmt.Errorf("unsupported guest action phase %q", phase)
	}
	transition, err := lock.Acquire(ctx, s.domain.StateRoot, "transition-"+string(domainID)+"-"+string(name))
	if err != nil {
		return ActionAttempt{}, fmt.Errorf("acquire session transition lock: %w", err)
	}
	defer func() { err = errors.Join(err, transition.Release()) }()
	held, err := lock.AcquireSession(ctx, s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return ActionAttempt{}, fmt.Errorf("acquire session lock: %w", err)
	}
	defer func() { err = errors.Join(err, held.Release()) }()
	record, err := LoadRecord(s.domain.StateRoot, string(domainID), string(name))
	if err != nil {
		return ActionAttempt{}, err
	}
	if record.IntendedState != StateRunning || record.Readiness.Status != ReadinessReady ||
		record.Backend.Kind != "tart" || record.RecipeIntentDigest == "" || !validUUID(record.StartGeneration) {
		return ActionAttempt{}, fmt.Errorf("action requires a recipe-bound running session")
	}
	if err := RequireNoRebuild(s.domain.StateRoot, domainID, string(name)); err != nil {
		return ActionAttempt{}, err
	}
	intentBytes, err := LoadRecipeIntent(s.domain.StateRoot, record.RecipeIntentDigest)
	if err != nil {
		return ActionAttempt{}, fmt.Errorf("load exact action recipe: %w", err)
	}
	intent, err := recipe.DecodeIntent(intentBytes)
	if err != nil {
		return ActionAttempt{}, err
	}
	var argv []string
	for _, step := range intent.Steps {
		if step.ID == actionID && step.Phase == phase {
			argv = append([]string(nil), step.Argv...)
			break
		}
	}
	if len(argv) == 0 {
		return ActionAttempt{}, fmt.Errorf("action is absent from exact stored recipe")
	}
	attemptID, err := s.newID()
	if err != nil {
		return ActionAttempt{}, fmt.Errorf("allocate action attempt: %w", err)
	}
	request := guestproto.ActionRequest{
		Version: guestproto.Version,
		Association: guestproto.Association{Domain: string(record.Domain), SessionID: record.ID,
			BackendKind: record.Backend.Kind, BackendObject: record.Backend.ObjectID},
		Generation: record.StartGeneration, RecipeDigest: record.RecipeIntentDigest,
		ActionID: actionID, ActionPhase: phase, AttemptID: attemptID, Argv: argv,
	}
	if _, _, err := guestproto.EncodeActionRequest(request); err != nil {
		return ActionAttempt{}, fmt.Errorf("stored recipe action is not executable: %w", err)
	}
	binding := startBinding(record)
	if err := s.requireFreshActionReady(ctx, binding); err != nil {
		return ActionAttempt{}, err
	}
	reserved := ActionAttempt{Version: 1, Domain: record.Domain, SessionName: string(name),
		SessionID: record.ID, BackendObject: record.Backend.ObjectID, Generation: record.StartGeneration,
		RecipeDigest: record.RecipeIntentDigest, ActionID: actionID, ActionPhase: phase,
		AttemptID: attemptID, State: ActionAttemptReserved}
	if err := ReserveActionAttempt(s.domain.StateRoot, reserved); err != nil {
		return ActionAttempt{}, err
	}
	result = reserved
	receipt, callErr := s.control.RunAction(ctx, binding, request)
	if callErr != nil {
		return s.indeterminateAction(reserved, fmt.Errorf("exact guest action: %w", callErr))
	}
	receiptBytes, receiptErr := guestproto.EncodeActionReceipt(request, receipt)
	if receiptErr != nil {
		return s.indeterminateAction(reserved, fmt.Errorf("invalid exact guest receipt: %w", receiptErr))
	}
	if err := s.requireFreshActionReady(ctx, binding); err != nil {
		return s.indeterminateAction(reserved, fmt.Errorf("fresh READY after guest action: %w", err))
	}
	digest := sha256.Sum256(receiptBytes)
	result.State = ActionAttemptSucceeded
	result.ReceiptSHA256 = fmt.Sprintf("%x", digest[:])
	if err := advanceActionAttempt(s.domain.StateRoot, reserved, result); err != nil {
		return s.indeterminateAction(reserved, fmt.Errorf("record exact action receipt: %w", err))
	}
	return result, nil
}

func (s *ActionService) requireFreshActionReady(ctx context.Context, binding supervisor.Binding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := s.control.Snapshot(ctx, binding)
	if err != nil {
		return fmt.Errorf("inspect exact action generation: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	now := s.now()
	if snapshot.Binding != binding || !snapshot.BackendRunning || !snapshot.SerialHealthy || !snapshot.PinPresent ||
		!snapshot.CertificateCurrent || !snapshot.ProbeOK || !snapshot.ZoneMatches || snapshot.ObservedAt.IsZero() ||
		snapshot.ObservedAt.After(now) || now.Sub(snapshot.ObservedAt) > maxReadySnapshotAge {
		return fmt.Errorf("action requires fresh exact-generation READY")
	}
	return nil
}

func (s *ActionService) indeterminateAction(reserved ActionAttempt, cause error) (ActionAttempt, error) {
	uncertain := reserved
	uncertain.State = ActionAttemptIndeterminate
	if err := advanceActionAttempt(s.domain.StateRoot, reserved, uncertain); err != nil {
		return reserved, errors.Join(cause, fmt.Errorf("record indeterminate action: %w", err))
	}
	return uncertain, cause
}
