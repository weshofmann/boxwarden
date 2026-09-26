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
	RetryAction(context.Context, supervisor.Binding, guestproto.ActionRequest) (guestproto.ActionReceipt, error)
}

// ActionService owns the durable host transition around guest-only steps.
// Public once/startup recipe admission stays closed until post-start routing
// and status reporting expose the automatic runner honestly.
type ActionService struct {
	domain  config.Domain
	control ActionControl
	now     func() time.Time
	newID   func() (string, error)
}

func NewActionService(configured config.Domain, control ActionControl) *ActionService {
	return &ActionService{domain: configured, control: control, now: time.Now, newID: newSessionID}
}

// RunAutomaticActions advances only the exact session returned by start. It
// reloads the durable plan after every checked receipt. ExecuteAction then
// independently rechecks the next step under the transition and session
// locks before reserving it, so a concurrent transition cannot turn a stale
// plan into guest execution.
func (s *ActionService) RunAutomaticActions(ctx context.Context, started Record) ([]ActionAttempt, error) {
	if s == nil || s.control == nil {
		return nil, fmt.Errorf("action service dependencies are required")
	}
	if started.Domain != s.domain.ID {
		return nil, fmt.Errorf("automatic action session differs from selected domain")
	}
	completed := make([]ActionAttempt, 0)
	for count := 0; count <= 128; count++ {
		current, pending, err := LoadAutomaticActionPlan(ctx, s.domain, string(started.Name))
		if err != nil {
			return completed, fmt.Errorf("load automatic action plan: %w", err)
		}
		if current != started {
			return completed, fmt.Errorf("automatic action session changed after start")
		}
		if len(pending) == 0 {
			return completed, nil
		}
		if count == 128 {
			return completed, fmt.Errorf("automatic action count exceeds recipe bound")
		}
		step := pending[0]
		attempt, runErr := s.ExecuteAction(ctx, string(started.Name), step.Phase, step.ID)
		if attempt.AttemptID != "" {
			completed = append(completed, attempt)
		}
		if runErr != nil {
			return completed, fmt.Errorf("automatic action %s/%s attempt %s: %w", step.Phase, step.ID, attempt.AttemptID, runErr)
		}
		if attempt.State != ActionAttemptSucceeded || attempt.ActionPhase != step.Phase || attempt.ActionID != step.ID {
			return completed, fmt.Errorf("automatic action %s/%s returned no checked success", step.Phase, step.ID)
		}
	}
	return completed, fmt.Errorf("automatic action count exceeds recipe bound")
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
	if phase == "once" || phase == "startup" {
		attempts, listErr := listActionAttemptsForRecord(s.domain.StateRoot, record)
		if listErr != nil {
			return ActionAttempt{}, fmt.Errorf("load exact automatic action journal: %w", listErr)
		}
		pending, planErr := PlanAutomaticActions(record, intent, attempts)
		if planErr != nil {
			return ActionAttempt{}, fmt.Errorf("plan automatic actions before reservation: %w", planErr)
		}
		if len(pending) == 0 || pending[0].Phase != phase || pending[0].ID != actionID {
			return ActionAttempt{}, fmt.Errorf("action %s/%s is not the next automatic step", phase, actionID)
		}
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

// RetryAction explicitly re-sends the exact original attempt. The guest's
// durable claim returns an existing success receipt, refuses an unresolved
// claim, or starts the command when no claim exists. This operation never
// allocates another attempt or changes argv.
func (s *ActionService) RetryAction(ctx context.Context, rawName, attemptID string) (result ActionAttempt, err error) {
	if s == nil || s.control == nil || s.now == nil {
		return ActionAttempt{}, fmt.Errorf("action service dependencies are required")
	}
	domainID, parseErr := domain.Parse(string(s.domain.ID))
	if parseErr != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return ActionAttempt{}, fmt.Errorf("invalid configured domain")
	}
	name, err := ParseName(rawName)
	if err != nil || !validUUID(attemptID) {
		return ActionAttempt{}, fmt.Errorf("invalid exact action retry identity")
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
	attempt, err := LoadActionAttempt(s.domain.StateRoot, domainID, record.ID, attemptID)
	if err != nil {
		return ActionAttempt{}, err
	}
	if attempt.State != ActionAttemptReserved && attempt.State != ActionAttemptIndeterminate {
		return attempt, fmt.Errorf("action attempt is already terminal")
	}
	if !actionMatchesRunningRecord(attempt, record) {
		return attempt, fmt.Errorf("action retry differs from current running generation")
	}
	if err := RequireNoRebuild(s.domain.StateRoot, domainID, string(name)); err != nil {
		return attempt, err
	}
	intentBytes, err := LoadRecipeIntent(s.domain.StateRoot, attempt.RecipeDigest)
	if err != nil {
		return attempt, err
	}
	intent, err := recipe.DecodeIntent(intentBytes)
	if err != nil {
		return attempt, err
	}
	var argv []string
	for _, step := range intent.Steps {
		if step.ID == attempt.ActionID && step.Phase == attempt.ActionPhase {
			argv = append([]string(nil), step.Argv...)
			break
		}
	}
	if len(argv) == 0 || attempt.ActionPhase == "launch" {
		return attempt, fmt.Errorf("action retry differs from exact guest-only recipe step")
	}
	request := guestproto.ActionRequest{Version: guestproto.Version,
		Association: guestproto.Association{Domain: string(attempt.Domain), SessionID: attempt.SessionID,
			BackendKind: record.Backend.Kind, BackendObject: attempt.BackendObject},
		Generation: attempt.Generation, RecipeDigest: attempt.RecipeDigest, ActionID: attempt.ActionID,
		ActionPhase: attempt.ActionPhase, AttemptID: attempt.AttemptID, Argv: argv}
	if _, _, err := guestproto.EncodeActionRequest(request); err != nil {
		return attempt, err
	}
	binding := startBinding(record)
	if err := s.requireFreshActionReady(ctx, binding); err != nil {
		return attempt, err
	}
	if attempt.State == ActionAttemptReserved {
		uncertain := attempt
		uncertain.State = ActionAttemptIndeterminate
		if err := advanceActionAttempt(s.domain.StateRoot, attempt, uncertain); err != nil {
			return attempt, fmt.Errorf("mark interrupted action indeterminate: %w", err)
		}
		attempt = uncertain
	}
	receipt, err := s.control.RetryAction(ctx, binding, request)
	if err != nil {
		return attempt, fmt.Errorf("exact guest action retry: %w", err)
	}
	receiptBytes, err := guestproto.EncodeActionReceipt(request, receipt)
	if err != nil {
		return attempt, fmt.Errorf("invalid exact guest receipt: %w", err)
	}
	if err := s.requireFreshActionReady(ctx, binding); err != nil {
		return attempt, fmt.Errorf("fresh READY after guest action retry: %w", err)
	}
	digest := sha256.Sum256(receiptBytes)
	result = attempt
	result.State = ActionAttemptSucceeded
	result.ReceiptSHA256 = fmt.Sprintf("%x", digest[:])
	if err := advanceActionAttempt(s.domain.StateRoot, attempt, result); err != nil {
		return attempt, fmt.Errorf("record exact recovered receipt: %w", err)
	}
	return result, nil
}
