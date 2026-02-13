package types

import "time"

// DomainEvent is the immutable record of a state transition.
type DomainEvent struct {
	ID              string
	AggregateType   string
	AggregateID     string
	WorkflowType    string
	WorkflowVersion int
	EventType       string
	CorrelationID   string
	CausationID     string
	ActorID         string
	StateBefore     map[string]any
	StateAfter      map[string]any
	TransitionName  string
	Error           map[string]any
	Payload         map[string]any
	CreatedAt       time.Time
	SequenceNum     int64
}
