package types

import "time"

// TaskDef defines a pending task created when entering a wait state.
type TaskDef struct {
	Type        string
	Description string
	Options     []string
	Timeout     time.Duration
}

// PendingTask is a human-in-the-loop decision awaiting completion.
type PendingTask struct {
	ID            string
	WorkflowType  string
	AggregateType string
	AggregateID   string
	CorrelationID string
	TaskType      string
	Description   string
	Options       []string
	State         string // the wait state this task was created for
	AssignedTo    string
	Timeout       time.Duration
	ExpiresAt     time.Time
	CompletedAt   *time.Time
	Choice        string
	CompletedBy   string
	Status        string
	CreatedAt     time.Time
}

// Task statuses.
const (
	TaskStatusPending   = "PENDING"
	TaskStatusCompleted = "COMPLETED"
	TaskStatusExpired   = "EXPIRED"
	TaskStatusCancelled = "CANCELLED"
)
