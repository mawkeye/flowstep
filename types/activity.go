package types

import "time"

// DispatchMode determines how an activity is dispatched.
type DispatchMode string

const (
	FireAndForget DispatchMode = "FIRE_AND_FORGET"
	AwaitResult   DispatchMode = "AWAIT_RESULT"
)

// ActivityInput is passed to Activity.Execute.
type ActivityInput struct {
	WorkflowType  string
	AggregateType string
	AggregateID   string
	CorrelationID string
	Params        map[string]any
	ScheduledAt   time.Time

	// Causation metadata — populated for entry/exit activities.
	Transition  string // Name of the transition that triggered this activity.
	SourceState string // Leaf state the workflow was in before the transition.
	EventID     string // ID of the domain event that triggered the transition (if any).
}

// ActivityResult is returned from Activity.Execute.
type ActivityResult struct {
	Output map[string]any
	Error  string
}

// RetryPolicy configures automatic retry for activities.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}

// ActivityInvocation tracks a dispatched activity.
type ActivityInvocation struct {
	ID            string
	ActivityName  string
	WorkflowType  string
	AggregateType string
	AggregateID   string
	CorrelationID string
	Mode          DispatchMode
	Input         ActivityInput
	RetryPolicy   *RetryPolicy
	ResultSignals map[string]string
	Timeout       time.Duration
	Status        string
	Result        *ActivityResult
	Attempts      int
	MaxAttempts   int
	NextRetryAt   *time.Time
	ScheduledAt   time.Time
	StartedAt     *time.Time
	CompletedAt   *time.Time
}

// Activity invocation statuses.
const (
	ActivityStatusScheduled = "SCHEDULED"
	ActivityStatusRunning   = "RUNNING"
	ActivityStatusCompleted = "COMPLETED"
	ActivityStatusFailed    = "FAILED"
	ActivityStatusTimedOut  = "TIMED_OUT"
)
