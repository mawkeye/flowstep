package types

import "time"

// Event type constants for side effect events.
const (
	// EventTypeSideEffect identifies a persisted non-deterministic operation result.
	// Used by engine.SideEffect() to capture UUID/time/random outputs for replay.
	EventTypeSideEffect = "SideEffect"

	// EventTypeActivityOutcome identifies a persisted activity result.
	// Stored before triggering OnSuccess/OnFailure transitions so replay can
	// determine which path was taken without re-executing the activity.
	EventTypeActivityOutcome = "ActivityOutcome"
)

// Payload key constants for SideEffect events (underscore prefix = engine-internal).
const (
	PayloadKeySideEffectName   = "_side_effect_name"
	PayloadKeySideEffectResult = "_side_effect_result"
)

// Payload key constants for ActivityOutcome events.
const (
	PayloadKeyActivityID      = "_activity_id"
	PayloadKeyActivityName    = "_activity_name"
	PayloadKeyActivityResult  = "_activity_result"
	PayloadKeyTransitionPath  = "_transition_path"
)

// NewSideEffectEvent builds a DomainEvent representing a persisted side effect result.
// The id parameter must be externally generated (e.g., via generateID() in internal/runtime).
func NewSideEffectEvent(
	id, aggregateType, aggregateID, workflowType string,
	workflowVersion int,
	correlationID, name string,
	result any,
	now time.Time,
) DomainEvent {
	return DomainEvent{
		ID:              id,
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		WorkflowType:    workflowType,
		WorkflowVersion: workflowVersion,
		EventType:       EventTypeSideEffect,
		CorrelationID:   correlationID,
		CreatedAt:       now,
		Payload: map[string]any{
			PayloadKeySideEffectName:   name,
			PayloadKeySideEffectResult: result,
		},
	}
}

// ParseSideEffect extracts the name and result from a SideEffect DomainEvent.
// Returns ok=false if the event is not a SideEffect event.
func ParseSideEffect(event DomainEvent) (name string, result any, ok bool) {
	if event.EventType != EventTypeSideEffect {
		return "", nil, false
	}
	n, _ := event.Payload[PayloadKeySideEffectName].(string)
	r := event.Payload[PayloadKeySideEffectResult]
	return n, r, true
}

// NewActivityOutcomeEvent builds a DomainEvent representing a persisted activity result.
// TransitionPath records which OnSuccess/OnFailure path was selected ("" if not yet wired).
// The id parameter must be externally generated.
func NewActivityOutcomeEvent(
	id, aggregateType, aggregateID, workflowType string,
	workflowVersion int,
	correlationID, activityID, activityName string,
	result ActivityResult,
	transitionPath string,
	now time.Time,
) DomainEvent {
	resultPayload := map[string]any{
		"output": result.Output,
		"error":  result.Error,
	}
	return DomainEvent{
		ID:              id,
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		WorkflowType:    workflowType,
		WorkflowVersion: workflowVersion,
		EventType:       EventTypeActivityOutcome,
		CorrelationID:   correlationID,
		CreatedAt:       now,
		Payload: map[string]any{
			PayloadKeyActivityID:     activityID,
			PayloadKeyActivityName:   activityName,
			PayloadKeyActivityResult: resultPayload,
			PayloadKeyTransitionPath: transitionPath,
		},
	}
}

// ParseActivityOutcome extracts activity outcome data from an ActivityOutcome DomainEvent.
// Returns ok=false if the event is not an ActivityOutcome event.
// Uses safe type assertions throughout to prevent panics on zero-value fields.
func ParseActivityOutcome(event DomainEvent) (activityID, activityName string, result ActivityResult, transitionPath string, ok bool) {
	if event.EventType != EventTypeActivityOutcome {
		return "", "", ActivityResult{}, "", false
	}

	activityID, _ = event.Payload[PayloadKeyActivityID].(string)
	activityName, _ = event.Payload[PayloadKeyActivityName].(string)
	transitionPath, _ = event.Payload[PayloadKeyTransitionPath].(string)

	if resultMap, ok2 := event.Payload[PayloadKeyActivityResult].(map[string]any); ok2 {
		result.Error, _ = resultMap["error"].(string)
		if output, ok3 := resultMap["output"].(map[string]any); ok3 {
			result.Output = output
		}
	}

	return activityID, activityName, result, transitionPath, true
}
