package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/weshofmann/boxwarden/internal/config"
	"github.com/weshofmann/boxwarden/internal/session"
)

type AlphaActionInput struct {
	Operation, SessionName, Phase, ActionID, AttemptID string
}

type AlphaActionFunc func(context.Context, config.Domain, AlphaActionInput) (session.ActionAttempt, error)

func validAlphaActionID(value string) bool {
	if len(value) < 1 || len(value) > 64 || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, c := range value[1:] {
		if c < 'a' || c > 'z' {
			if c < '0' || c > '9' {
				if c != '-' {
					return false
				}
			}
		}
	}
	return true
}

func writeAlphaAction(output io.Writer, selected config.Domain, input AlphaActionInput, result session.ActionAttempt, operationErr error) error {
	if result.AttemptID == "" {
		if operationErr == nil {
			return fmt.Errorf("action returned no attempt or result")
		}
		return operationErr
	}
	if result.Version != 1 || result.Domain != selected.ID || result.SessionName != input.SessionName ||
		!alphaCreateUUID(result.AttemptID) || result.ActionID == "" || result.ActionPhase == "" ||
		(input.AttemptID != "" && result.AttemptID != input.AttemptID) ||
		(input.Operation == "run" && (result.ActionPhase != input.Phase || result.ActionID != input.ActionID)) {
		return errors.Join(fmt.Errorf("action returned a result outside the selected intent"), operationErr)
	}
	if operationErr == nil {
		if input.Operation == "skip" && result.State != session.ActionAttemptSkipped ||
			input.Operation != "skip" && result.State != session.ActionAttemptSucceeded {
			return fmt.Errorf("action returned a nonterminal result without an error")
		}
	}
	if result.State == session.ActionAttemptSucceeded {
		if !lowerSHA(result.ReceiptSHA256) {
			return fmt.Errorf("successful action lacks an exact receipt digest")
		}
	} else if result.ReceiptSHA256 != "" {
		return fmt.Errorf("non-successful action claimed a receipt digest")
	}
	if result.State != session.ActionAttemptReserved && result.State != session.ActionAttemptIndeterminate &&
		result.State != session.ActionAttemptSucceeded && result.State != session.ActionAttemptSkipped {
		return fmt.Errorf("action returned an unsupported state %q", result.State)
	}
	if _, err := fmt.Fprintf(output, "domain: %s\nsession: %s\nattempt: %s\naction: %s/%s\nstate: %s\n",
		result.Domain, result.SessionName, result.AttemptID, result.ActionPhase, result.ActionID, result.State); err != nil {
		return err
	}
	if result.State == session.ActionAttemptSucceeded {
		if _, err := fmt.Fprintf(output, "receipt-sha256: %s\n", result.ReceiptSHA256); err != nil {
			return err
		}
	} else if result.State == session.ActionAttemptReserved || result.State == session.ActionAttemptIndeterminate {
		if _, err := fmt.Fprintf(output, "recovery: if the same generation is READY, session action retry %s %s; to skip, stop the session first, then session action skip %s %s\n",
			result.AttemptID, result.SessionName, result.AttemptID, result.SessionName); err != nil {
			return err
		}
	}
	return operationErr
}
