package flowstate

import (
	"errors"

	"github.com/mawkeye/flowstate/types"
)

// Definition errors (Build time).
var (
	ErrNoInitialState        = errors.New("flowstate: no initial state defined")
	ErrMultipleInitialStates = errors.New("flowstate: multiple initial states")
	ErrNoTerminalStates      = errors.New("flowstate: no terminal states defined")
	ErrUnreachableState      = errors.New("flowstate: unreachable state detected")
	ErrDeadEndState          = errors.New("flowstate: non-terminal state with no outgoing transitions")
	ErrUnknownState          = errors.New("flowstate: transition references unknown state")
	ErrMissingDefault        = errors.New("flowstate: routed transition missing Default")
	ErrDuplicateTransition   = errors.New("flowstate: duplicate transition name")
)

// Runtime errors.
var (
	ErrInstanceNotFound       = errors.New("flowstate: workflow instance not found")
	ErrInvalidTransition      = errors.New("flowstate: transition not valid from current state")
	ErrGuardFailed            = types.ErrGuardFailed
	ErrNoMatchingRoute        = errors.New("flowstate: no condition matched and no default")
	ErrAlreadyTerminal        = errors.New("flowstate: workflow already in terminal state")
	ErrWorkflowStuck          = errors.New("flowstate: workflow is stuck")
	ErrConcurrentModification = errors.New("flowstate: concurrent modification detected")
	ErrEngineShutdown         = errors.New("flowstate: engine is shut down")
)

// Signal errors.
var (
	ErrNoMatchingSignal = errors.New("flowstate: no transition matches signal")
	ErrSignalAmbiguous  = errors.New("flowstate: multiple transitions match signal")
)

// Task errors.
var (
	ErrTaskNotFound         = errors.New("flowstate: pending task not found")
	ErrTaskExpired          = errors.New("flowstate: task has expired")
	ErrTaskAlreadyCompleted = errors.New("flowstate: task already completed")
	ErrInvalidChoice        = errors.New("flowstate: choice not in task options")
)

// Activity errors.
var (
	ErrActivityNotRegistered = errors.New("flowstate: activity not registered")
	ErrActivityTimeout       = errors.New("flowstate: activity timed out")
	ErrActivityNotFound      = errors.New("flowstate: activity invocation not found")
)

// GuardError is a type alias for types.GuardError. The engine returns *GuardError
// for guard failures so callers can use errors.As to extract the guard name and
// reason, and errors.Is against ErrGuardFailed.
type GuardError = types.GuardError
