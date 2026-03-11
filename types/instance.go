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
	// ShallowHistory records the last direct child visited for each compound state.
	// Key: compound state name. Value: last direct child state name.
	ShallowHistory map[string]string
	// DeepHistory records the last leaf descendant visited for each compound state.
	// Key: compound state name. Value: last leaf state name.
	DeepHistory map[string]string
	// LastReadUpdatedAt is set by the engine before modifying UpdatedAt.
	// Store adapters use it as the expected current updated_at in their
	// optimistic locking WHERE clause. Not persisted (json:"-").
	LastReadUpdatedAt time.Time `json:"-"`
}
