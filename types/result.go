package types

// PostCommitWarning records a non-fatal error from a post-commit operation.
// The original error is preserved so callers can use errors.Is / errors.As.
type PostCommitWarning struct {
	Operation string // e.g. "EventBus.Emit", "ActivityRunner.Dispatch"
	Err       error
}

// TransitionResult is returned by every successful state advancement.
type TransitionResult struct {
	Instance             WorkflowInstance
	Event                DomainEvent
	PreviousState        string
	NewState             string
	TransitionName       string
	ActivitiesDispatched []string
	TaskCreated          *PendingTask
	ChildrenSpawned      []ChildRelation
	IsTerminal           bool
	// Warnings collects non-fatal errors from post-commit operations such as
	// EventBus.Emit or ActivityRunner.Dispatch. The transition is still
	// considered successful — the state has been committed to the store.
	Warnings []PostCommitWarning
}
