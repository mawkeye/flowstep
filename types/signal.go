package types

// SignalInput is sent to engine.Signal() to trigger a transition.
type SignalInput struct {
	TargetAggregateType string
	TargetAggregateID   string
	SignalName          string
	Payload             map[string]any
	ActorID             string
}
