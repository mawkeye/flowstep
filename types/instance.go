package types

import "time"

// WorkflowInstance tracks the live state of a workflow.
type WorkflowInstance struct {
	ID              string
	WorkflowType    string
	WorkflowVersion int
	AggregateType   string
	AggregateID     string
	CurrentState    string
	StateData       map[string]any
	CorrelationID   string
	IsStuck         bool
	StuckReason     string
	StuckAt         *time.Time
	RetryCount      int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
