package types

import "errors"

// ErrGuardFailed is the sentinel error returned when a guard check fails.
// Re-exported as flowstate.ErrGuardFailed in the root package.
var ErrGuardFailed = errors.New("guard check failed")

// GuardError wraps a guard failure with the name of the failing guard and
// the reason returned by its Check method. The engine returns *GuardError
// for all guard failures so callers can use errors.Is against ErrGuardFailed
// and errors.As to extract guard details.
type GuardError struct {
	GuardName string
	Reason    error
}

// Error implements the error interface.
func (e *GuardError) Error() string {
	return "flowstate: guard " + e.GuardName + " failed: " + e.Reason.Error()
}

// Is reports true when target is ErrGuardFailed so that errors.Is works.
func (e *GuardError) Is(target error) bool {
	return target == ErrGuardFailed
}

// Unwrap returns the underlying guard failure reason.
func (e *GuardError) Unwrap() error {
	return e.Reason
}
