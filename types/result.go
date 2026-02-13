package types

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
}
