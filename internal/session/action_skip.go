package session

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/weshofmann/boxwarden/internal/domain"
	"github.com/weshofmann/boxwarden/internal/lock"
)

// SkipAction records a deliberate decision to continue without claiming that
// the guest command did not run. The normal stop transition must have proved
// the exact backend stopped first, so no old command can still be active.
func (s *ActionService) SkipAction(ctx context.Context, rawName, attemptID string) (result ActionAttempt, err error) {
	if s == nil {
		return ActionAttempt{}, fmt.Errorf("action service is required")
	}
	domainID, parseErr := domain.Parse(string(s.domain.ID))
	if parseErr != nil || domainID != s.domain.ID || strings.TrimSpace(s.domain.StateRoot) == "" {
		return ActionAttempt{}, fmt.Errorf("invalid configured domain")
	}
	name, err := ParseName(rawName)
	if err != nil || !validUUID(attemptID) {
		return ActionAttempt{}, fmt.Errorf("invalid exact action skip identity")
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
	if !actionMatchesStoppedRecord(attempt, record) {
		return attempt, fmt.Errorf("action skip requires the exact stopped system")
	}
	if attempt.State == ActionAttemptSkipped {
		return attempt, nil
	}
	if attempt.State != ActionAttemptReserved && attempt.State != ActionAttemptIndeterminate {
		return attempt, fmt.Errorf("action attempt cannot be skipped from %q", attempt.State)
	}
	result = attempt
	result.State = ActionAttemptSkipped
	if err := advanceActionAttempt(s.domain.StateRoot, attempt, result); err != nil {
		return attempt, fmt.Errorf("record explicit action skip: %w", err)
	}
	return result, nil
}
