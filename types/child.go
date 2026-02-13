package types

import "time"

// ChildDef defines a single child workflow to spawn.
type ChildDef struct {
	WorkflowType string
	InputFrom    string
}

// ChildrenDef defines parallel child workflows to spawn.
type ChildrenDef struct {
	WorkflowType string
	InputsFn     func(aggregate any) []map[string]any
}

// ChildRelation tracks a parent-child workflow relationship.
type ChildRelation struct {
	ID                  string
	GroupID             string
	ParentWorkflowType  string
	ParentAggregateType string
	ParentAggregateID   string
	ChildWorkflowType   string
	ChildAggregateType  string
	ChildAggregateID    string
	CorrelationID       string
	Status              string
	ChildTerminalState  string
	JoinPolicy          string
	CreatedAt           time.Time
	CompletedAt         *time.Time
}

// JoinPolicy defines when a parent should resume after spawning parallel children.
type JoinPolicy struct {
	Mode           string // ALL, ANY, N
	TerminalState  string // for ANY mode
	Count          int    // for N mode
	CancelSiblings bool
}

// JoinAll waits for ALL children to reach any terminal state.
func JoinAll() JoinPolicy {
	return JoinPolicy{Mode: "ALL"}
}

// JoinAny resumes when ANY child reaches the specified terminal state.
func JoinAny(terminalState string) JoinPolicy {
	return JoinPolicy{Mode: "ANY", TerminalState: terminalState}
}

// JoinN resumes when N children reach any terminal state.
func JoinN(n int) JoinPolicy {
	return JoinPolicy{Mode: "N", Count: n}
}

// CancelRemaining signals remaining children to cancel after join is satisfied.
func (j JoinPolicy) CancelRemaining() JoinPolicy {
	j.CancelSiblings = true
	return j
}
