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

// Guard checks preconditions before a transition. MUST be deterministic.
// Only read from aggregate and params. No API calls, no DB queries, no I/O.
// Return nil to pass. Return error to block.
type Guard interface {
	Check(ctx context.Context, aggregate any, params map[string]any) error
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
