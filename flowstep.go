package flowstep

import (
	"github.com/mawkeye/flowstep/internal/builder"
	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// Re-export builder option types for the public API.
type (
	TransitionOption  = builder.TransitionOption
	StateOption       = builder.StateOption
	PostCommitWarning = types.PostCommitWarning
)

// Re-export builder functions for the fluent API.
var (
	From               = builder.From
	To                 = builder.To
	Event              = builder.Event
	Guards             = builder.Guards
	OnSignal           = builder.OnSignal
	OnTaskCompleted    = builder.OnTaskCompleted
	OnChildCompleted   = builder.OnChildCompleted
	OnChildrenJoined   = builder.OnChildrenJoined
	OnTimeout          = builder.OnTimeout
	EmitTask           = builder.EmitTask
	SpawnChild         = builder.SpawnChild
	SpawnChildren      = builder.SpawnChildren
	AllowSelfTransition = builder.AllowSelfTransition
	Dispatch           = builder.Dispatch
	DispatchAndWait    = builder.DispatchAndWait
	Route              = builder.Route
	When               = builder.When
	Default            = builder.Default

	Initial   = builder.Initial
	Terminal  = builder.Terminal
	State     = builder.State
	WaitState = builder.WaitState
)

// Define starts building a workflow definition.
func Define(aggregateType, workflowType string) *builder.DefBuilder {
	validateFn := func(def *types.Definition) error {
		return graph.Validate(def, graph.Sentinels{
			ErrNoInitialState:        ErrNoInitialState,
			ErrMultipleInitialStates: ErrMultipleInitialStates,
			ErrNoTerminalStates:      ErrNoTerminalStates,
			ErrUnreachableState:      ErrUnreachableState,
			ErrDeadEndState:          ErrDeadEndState,
			ErrUnknownState:          ErrUnknownState,
			ErrMissingDefault:        ErrMissingDefault,
			ErrDuplicateTransition:   ErrDuplicateTransition,
		})
	}
	return builder.New(aggregateType, workflowType, validateFn, ErrDuplicateTransition)
}
