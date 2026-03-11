package flowstep

import (
	"errors"

	"github.com/mawkeye/flowstep/types"
)

// Definition errors (Build time).
var (
	ErrNoInitialState        = errors.New("flowstep: no initial state defined")
	ErrMultipleInitialStates = errors.New("flowstep: multiple initial states")
	ErrNoTerminalStates      = errors.New("flowstep: no terminal states defined")
	ErrUnreachableState      = errors.New("flowstep: unreachable state detected")
	ErrDeadEndState          = errors.New("flowstep: non-terminal state with no outgoing transitions")
	ErrUnknownState          = errors.New("flowstep: transition references unknown state")
	ErrMissingDefault        = errors.New("flowstep: routed transition missing Default")
	ErrDuplicateTransition   = errors.New("flowstep: duplicate transition name")
)

// Runtime errors.
var (
	ErrInstanceNotFound       = errors.New("flowstep: workflow instance not found")
	ErrInvalidTransition      = errors.New("flowstep: transition not valid from current state")
	ErrGuardFailed            = types.ErrGuardFailed
	ErrNoMatchingRoute        = errors.New("flowstep: no condition matched and no default")
	ErrAlreadyTerminal        = errors.New("flowstep: workflow already in terminal state")
	ErrWorkflowStuck          = errors.New("flowstep: workflow is stuck")
	ErrConcurrentModification = errors.New("flowstep: concurrent modification detected")
	ErrEngineShutdown         = errors.New("flowstep: engine is shut down")
)

// Signal errors.
var (
	ErrNoMatchingSignal = errors.New("flowstep: no transition matches signal")
	ErrSignalAmbiguous  = errors.New("flowstep: multiple transitions match signal")
)

// Task errors.
var (
	ErrTaskNotFound         = errors.New("flowstep: pending task not found")
	ErrTaskExpired          = errors.New("flowstep: task has expired")
	ErrTaskAlreadyCompleted = errors.New("flowstep: task already completed")
	ErrInvalidChoice        = errors.New("flowstep: choice not in task options")
)

// Graph compilation errors.
var (
	ErrSpawnCycle                  = errors.New("flowstep: cross-workflow spawn cycle detected")
	ErrCompoundStateNoInitialChild = errors.New("flowstep: compound state has no InitialChild")
	ErrOrphanedChild               = errors.New("flowstep: state references non-existent parent")
	ErrCircularHierarchy           = errors.New("flowstep: circular parent-child hierarchy detected")
)

// Activity errors.
var (
	ErrActivityNotRegistered = errors.New("flowstep: activity not registered")
	ErrActivityTimeout       = errors.New("flowstep: activity timed out")
	ErrActivityNotFound      = errors.New("flowstep: activity invocation not found")
)

// GuardError is a type alias for types.GuardError. The engine returns *GuardError
// for guard failures so callers can use errors.As to extract the guard name and
// reason, and errors.Is against ErrGuardFailed.
type GuardError = types.GuardError
