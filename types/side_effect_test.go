package types_test

import (
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

func TestNewSideEffectEvent_RoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	result := map[string]any{"uuid": "abc-123"}

	event := types.NewSideEffectEvent(
		"evt-001",
		"order", "order-42",
		"OrderWorkflow", 1,
		"corr-99",
		"GenerateOrderID",
		result,
		now,
	)

	if event.EventType != types.EventTypeSideEffect {
		t.Errorf("EventType = %q, want %q", event.EventType, types.EventTypeSideEffect)
	}
	if event.ID != "evt-001" {
		t.Errorf("ID = %q, want %q", event.ID, "evt-001")
	}
	if event.AggregateType != "order" {
		t.Errorf("AggregateType = %q, want %q", event.AggregateType, "order")
	}
	if event.AggregateID != "order-42" {
		t.Errorf("AggregateID = %q, want %q", event.AggregateID, "order-42")
	}
	if event.WorkflowType != "OrderWorkflow" {
		t.Errorf("WorkflowType = %q, want %q", event.WorkflowType, "OrderWorkflow")
	}
	if event.WorkflowVersion != 1 {
		t.Errorf("WorkflowVersion = %d, want 1", event.WorkflowVersion)
	}
	if event.CorrelationID != "corr-99" {
		t.Errorf("CorrelationID = %q, want %q", event.CorrelationID, "corr-99")
	}
	if event.CreatedAt != now {
		t.Errorf("CreatedAt = %v, want %v", event.CreatedAt, now)
	}

	// Parse back
	name, got, ok := types.ParseSideEffect(event)
	if !ok {
		t.Fatal("ParseSideEffect: ok = false, want true")
	}
	if name != "GenerateOrderID" {
		t.Errorf("name = %q, want %q", name, "GenerateOrderID")
	}
	gotMap, ok2 := got.(map[string]any)
	if !ok2 {
		t.Fatalf("result type = %T, want map[string]any", got)
	}
	if gotMap["uuid"] != "abc-123" {
		t.Errorf("result[uuid] = %q, want %q", gotMap["uuid"], "abc-123")
	}
}

func TestParseSideEffect_WrongEventType(t *testing.T) {
	event := types.DomainEvent{
		EventType: "SomeOtherEvent",
		Payload:   map[string]any{types.PayloadKeySideEffectName: "foo"},
	}
	_, _, ok := types.ParseSideEffect(event)
	if ok {
		t.Error("ParseSideEffect: ok = true for wrong EventType, want false")
	}
}

func TestNewActivityOutcomeEvent_RoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	result := types.ActivityResult{
		Output: map[string]any{"invoiceID": "inv-99"},
		Error:  "timeout error",
	}

	event := types.NewActivityOutcomeEvent(
		"evt-002",
		"order", "order-42",
		"OrderWorkflow", 1,
		"corr-99",
		"act-invoc-001",
		"ProcessPayment",
		result,
		"OnFailure",
		now,
	)

	if event.EventType != types.EventTypeActivityOutcome {
		t.Errorf("EventType = %q, want %q", event.EventType, types.EventTypeActivityOutcome)
	}

	actID, actName, gotResult, transPath, ok := types.ParseActivityOutcome(event)
	if !ok {
		t.Fatal("ParseActivityOutcome: ok = false, want true")
	}
	if actID != "act-invoc-001" {
		t.Errorf("activityID = %q, want %q", actID, "act-invoc-001")
	}
	if actName != "ProcessPayment" {
		t.Errorf("activityName = %q, want %q", actName, "ProcessPayment")
	}
	if transPath != "OnFailure" {
		t.Errorf("transitionPath = %q, want %q", transPath, "OnFailure")
	}
	if gotResult.Error != "timeout error" {
		t.Errorf("result.Error = %q, want %q", gotResult.Error, "timeout error")
	}
	out, ok2 := gotResult.Output["invoiceID"]
	if !ok2 || out != "inv-99" {
		t.Errorf("result.Output[invoiceID] = %v, want %q", out, "inv-99")
	}
}

func TestNewActivityOutcomeEvent_EmptyErrorField(t *testing.T) {
	now := time.Now()
	result := types.ActivityResult{
		Output: map[string]any{"key": "val"},
		Error:  "", // empty — zero value
	}

	event := types.NewActivityOutcomeEvent(
		"evt-003", "order", "order-1", "OrderWorkflow", 1, "corr-1",
		"act-001", "SomeActivity", result, "OnSuccess", now,
	)

	_, _, gotResult, _, ok := types.ParseActivityOutcome(event)
	if !ok {
		t.Fatal("ParseActivityOutcome: ok = false for empty Error field")
	}
	if gotResult.Error != "" {
		t.Errorf("result.Error = %q, want empty string", gotResult.Error)
	}
	if gotResult.Output["key"] != "val" {
		t.Errorf("result.Output[key] = %v, want %q", gotResult.Output["key"], "val")
	}
}

func TestParseActivityOutcome_WrongEventType(t *testing.T) {
	event := types.DomainEvent{
		EventType: "SideEffect",
		Payload:   map[string]any{},
	}
	_, _, _, _, ok := types.ParseActivityOutcome(event)
	if ok {
		t.Error("ParseActivityOutcome: ok = true for wrong EventType, want false")
	}
}

// TestParseSideEffect_MissingPayloadKeys documents the contract: ParseSideEffect returns
// ok=true as long as the EventType matches, even if payload keys are absent. The caller
// receives empty name and nil result. This is intentional — the function is permissive;
// callers that need strict validation should check the name/result themselves.
func TestParseSideEffect_MissingPayloadKeys(t *testing.T) {
	event := types.DomainEvent{
		EventType: types.EventTypeSideEffect,
		Payload:   map[string]any{}, // no name or result keys
	}
	name, result, ok := types.ParseSideEffect(event)
	if !ok {
		t.Error("ParseSideEffect: ok = false for missing keys, expected ok=true (permissive)")
	}
	if name != "" {
		t.Errorf("name = %q, want empty string for missing key", name)
	}
	if result != nil {
		t.Errorf("result = %v, want nil for missing key", result)
	}
}

func TestPayloadKeysAreSet(t *testing.T) {
	now := time.Now()
	result := map[string]any{"x": 1}

	event := types.NewSideEffectEvent(
		"id", "agg", "id", "wf", 1, "corr", "MyEffect", result, now,
	)

	if _, ok := event.Payload[types.PayloadKeySideEffectName]; !ok {
		t.Errorf("payload missing key %q", types.PayloadKeySideEffectName)
	}
	if _, ok := event.Payload[types.PayloadKeySideEffectResult]; !ok {
		t.Errorf("payload missing key %q", types.PayloadKeySideEffectResult)
	}
}
