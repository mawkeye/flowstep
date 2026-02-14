package testutil

import (
	"context"
	"testing"
)

// AssertState verifies that the workflow instance is in the expected state.
func AssertState(t *testing.T, te *TestEngine, aggregateType, aggregateID, expected string) {
	t.Helper()
	inst, err := te.InstanceStore.Get(context.Background(), aggregateType, aggregateID)
	if err != nil {
		t.Fatalf("testutil: get instance %s/%s: %v", aggregateType, aggregateID, err)
	}
	if inst.CurrentState != expected {
		t.Errorf("testutil: expected state %q for %s/%s, got %q", expected, aggregateType, aggregateID, inst.CurrentState)
	}
}

// AssertEventCount verifies the number of events recorded for an aggregate.
func AssertEventCount(t *testing.T, te *TestEngine, aggregateType, aggregateID string, expected int) {
	t.Helper()
	events, err := te.EventStore.ListByAggregate(context.Background(), aggregateType, aggregateID)
	if err != nil {
		t.Fatalf("testutil: list events for %s/%s: %v", aggregateType, aggregateID, err)
	}
	if len(events) != expected {
		t.Errorf("testutil: expected %d events for %s/%s, got %d", expected, aggregateType, aggregateID, len(events))
	}
}

// AssertEventChain verifies the sequence of event types for a correlation ID.
func AssertEventChain(t *testing.T, te *TestEngine, correlationID string, expectedEventTypes ...string) {
	t.Helper()
	events, err := te.EventStore.ListByCorrelation(context.Background(), correlationID)
	if err != nil {
		t.Fatalf("testutil: list events by correlation %s: %v", correlationID, err)
	}
	if len(events) != len(expectedEventTypes) {
		t.Fatalf("testutil: expected %d events in chain, got %d", len(expectedEventTypes), len(events))
	}
	for i, expected := range expectedEventTypes {
		if events[i].EventType != expected {
			t.Errorf("testutil: event[%d] expected type %q, got %q", i, expected, events[i].EventType)
		}
	}
}
