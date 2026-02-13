package flowstate

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors(t *testing.T) {
	// Verify sentinel errors are distinct
	sentinels := []error{
		ErrNoInitialState,
		ErrMultipleInitialStates,
		ErrNoTerminalStates,
		ErrUnreachableState,
		ErrDeadEndState,
		ErrUnknownState,
		ErrMissingDefault,
		ErrDuplicateTransition,
		ErrInstanceNotFound,
		ErrInvalidTransition,
		ErrGuardFailed,
		ErrNoMatchingRoute,
		ErrAlreadyTerminal,
		ErrWorkflowStuck,
		ErrConcurrentModification,
		ErrNoMatchingSignal,
		ErrSignalAmbiguous,
		ErrTaskNotFound,
		ErrTaskExpired,
		ErrTaskAlreadyCompleted,
		ErrInvalidChoice,
		ErrActivityNotRegistered,
		ErrActivityTimeout,
	}

	for i, a := range sentinels {
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Errorf("sentinel errors %d and %d should be distinct", i, j)
			}
		}
	}
}

func TestGuardError(t *testing.T) {
	inner := fmt.Errorf("payment not complete")
	err := &GuardError{GuardName: "PaymentCompleteGuard", Reason: inner}

	if !errors.Is(err, ErrGuardFailed) {
		t.Error("GuardError should match ErrGuardFailed via errors.Is")
	}

	var guardErr *GuardError
	if !errors.As(err, &guardErr) {
		t.Error("should unwrap to *GuardError")
	}
	if guardErr.GuardName != "PaymentCompleteGuard" {
		t.Errorf("expected PaymentCompleteGuard, got %s", guardErr.GuardName)
	}
}
