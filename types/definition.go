package types

import (
	"context"
	"time"
)

// TriggerType determines how a transition is fired.
type TriggerType string

const (
	TriggerDirect          TriggerType = "DIRECT"
	TriggerSignal          TriggerType = "SIGNAL"
	TriggerTaskCompleted   TriggerType = "TASK_COMPLETED"
	TriggerChildCompleted  TriggerType = "CHILD_COMPLETED"
	TriggerChildrenJoined  TriggerType = "CHILDREN_JOINED"
	TriggerTimeout         TriggerType = "TIMEOUT"
)

// HistoryMode determines how a compound state records and restores its last-active child.
type HistoryMode string

const (
	// HistoryShallow remembers the last direct child of the compound state.
	HistoryShallow HistoryMode = "SHALLOW"
	// HistoryDeep remembers the last leaf descendant of the compound state.
	HistoryDeep HistoryMode = "DEEP"
)

// Guard checks preconditions before a transition. MUST be deterministic.
// Only read from aggregate and params. No API calls, no DB queries, no I/O.
// Return nil to pass. Return error to block.
type Guard interface {
	Check(ctx context.Context, aggregate any, params map[string]any) error
}

// NamedGuard is an optional extension of Guard that provides a human-readable name.
// When a guard implements NamedGuard, its Name() is used in observer events instead
// of the default fmt.Sprintf("%T", guard) fallback. Backward-compatible: guards
// without this interface continue to work unchanged.
type NamedGuard interface {
	Guard
	Name() string
}

// Condition evaluates a routing decision. MUST be deterministic.
// Only read from aggregate and params. No I/O.
type Condition interface {
	Evaluate(ctx context.Context, aggregate any, params map[string]any) (bool, error)
}

// StateDef represents a named state in a workflow.
type StateDef struct {
	Name       string
	IsInitial  bool
	IsTerminal bool
	IsWait     bool

	// Hierarchy fields — zero values represent flat (non-hierarchical) states.
	Parent        string   // Name of the parent compound state. Empty for root states.
	Children      []string // Direct child state names. Populated by Build(). Empty for leaf states.
	InitialChild  string   // Name of the child state to enter when this compound state is entered.
	IsCompound    bool     // True if this state has children (set by Build() or explicitly).
	IsParallel    bool     // True if this is a parallel (orthogonal regions) state. Implies IsCompound.
	EntryActivity string   // Activity name to execute synchronously when entering this state.
	ExitActivity  string   // Activity name to execute synchronously when exiting this state.
}

// Route represents a conditional target in a routed transition.
type Route struct {
	Condition Condition
	Target    string
	IsDefault bool
}

// ActivityDef describes an activity to dispatch on a transition.
type ActivityDef struct {
	Name          string
	Mode          DispatchMode
	RetryPolicy   *RetryPolicy
	ResultSignals map[string]string // result key -> signal name
	Timeout       time.Duration
}

// TransitionDef represents a named edge between states.
type TransitionDef struct {
	Name                string
	Sources             []string
	Target              string
	Routes              []Route
	Event               string
	Guards              []Guard
	Activities          []ActivityDef
	TaskDef             *TaskDef
	ChildDef            *ChildDef
	ChildrenDef         *ChildrenDef
	TriggerType         TriggerType
	TriggerKey          string
	AllowSelfTransition bool
	HistoryMode         HistoryMode
	Priority            int // Higher value = higher priority. Used for deterministic conflict resolution in parallel states.
}

// Definition is an immutable, validated workflow definition.
// Created via the builder. Cannot be modified after Build().
type Definition struct {
	AggregateType  string
	WorkflowType   string
	Version        int
	States         map[string]StateDef
	Transitions    map[string]TransitionDef
	InitialState   string
	TerminalStates []string
	Warnings       []string
}
