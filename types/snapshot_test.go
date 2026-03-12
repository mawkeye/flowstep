package types_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

// instanceToSnapshotFieldMap maps WorkflowInstance field names to their Snapshot equivalents.
// Fields intentionally excluded from Snapshot are listed in instanceExcludedFields.
// This mapping is the authoritative contract; the test enforces it via reflect.
var instanceToSnapshotFieldMap = map[string]string{
	"ID":               "InstanceID",
	"WorkflowType":     "WorkflowType",
	"WorkflowVersion":  "WorkflowVersion",
	"AggregateType":    "AggregateType",
	"AggregateID":      "AggregateID",
	"CurrentState":     "CurrentState",
	"StateData":        "StateData",
	"CorrelationID":    "CorrelationID",
	"IsStuck":          "IsStuck",
	"StuckReason":      "StuckReason",
	"StuckAt":          "StuckAt",
	"RetryCount":       "RetryCount",
	"CreatedAt":        "CreatedAt",
	"ShallowHistory":   "ShallowHistory",
	"DeepHistory":      "DeepHistory",
	"ActiveInParallel": "ActiveInParallel",
	"ParallelClock":    "ParallelClock",
}

// instanceExcludedFields are WorkflowInstance fields intentionally absent from Snapshot.
var instanceExcludedFields = map[string]bool{
	"UpdatedAt":         true,
	"LastReadUpdatedAt": true,
}

// TestSnapshotFieldMapping verifies that Snapshot covers every WorkflowInstance field
// (minus the excluded set) using an explicit name-to-name mapping table.
// This test fails if a new field is added to WorkflowInstance without updating Snapshot.
func TestSnapshotFieldMapping(t *testing.T) {
	instanceType := reflect.TypeFor[types.WorkflowInstance]()
	snapshotType := reflect.TypeFor[types.Snapshot]()

	// Build set of Snapshot field names for O(1) lookup.
	snapshotFields := make(map[string]bool, snapshotType.NumField())
	for i := range snapshotType.NumField() {
		snapshotFields[snapshotType.Field(i).Name] = true
	}

	// Every WorkflowInstance field must either be excluded or mapped to an existing Snapshot field.
	for i := range instanceType.NumField() {
		f := instanceType.Field(i)
		if instanceExcludedFields[f.Name] {
			continue
		}
		snapName, ok := instanceToSnapshotFieldMap[f.Name]
		if !ok {
			t.Errorf("WorkflowInstance.%s has no mapping in instanceToSnapshotFieldMap and is not excluded — add it to the map or instanceExcludedFields", f.Name)
			continue
		}
		if !snapshotFields[snapName] {
			t.Errorf("instanceToSnapshotFieldMap maps WorkflowInstance.%s → Snapshot.%s, but Snapshot.%s does not exist", f.Name, snapName, snapName)
		}
	}

	// Every mapped Snapshot field must exist on Snapshot.
	for instanceField, snapField := range instanceToSnapshotFieldMap {
		if !snapshotFields[snapField] {
			t.Errorf("instanceToSnapshotFieldMap maps WorkflowInstance.%s → Snapshot.%s, but Snapshot.%s does not exist", instanceField, snapField, snapField)
		}
	}
}

// TestSnapshotJSONRoundTrip verifies that a fully-populated Snapshot survives JSON serialization.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	stuckAt := now.Add(-5 * time.Minute)

	original := types.Snapshot{
		InstanceID:      "inst-123",
		WorkflowType:    "order",
		WorkflowVersion: 2,
		AggregateType:   "Order",
		AggregateID:     "order-abc",
		CurrentState:    "processing",
		StateData: map[string]any{
			"amount":   float64(100),
			"currency": "USD",
		},
		CorrelationID: "corr-xyz",
		IsStuck:       true,
		StuckReason:   "entry activity failed",
		StuckAt:       &stuckAt,
		RetryCount:    3,
		ShallowHistory: map[string]string{
			"parent": "child-a",
		},
		DeepHistory: map[string]string{
			"root": "leaf-x",
		},
		ActiveInParallel: map[string]string{
			"region-a": "state-1",
			"region-b": "state-2",
		},
		ParallelClock:  7,
		CreatedAt:      now.Add(-1 * time.Hour),
		DefinitionHash: "abc123def456",
		CapturedAt:     now,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var restored types.Snapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Compare all fields.
	if restored.InstanceID != original.InstanceID {
		t.Errorf("InstanceID: got %q want %q", restored.InstanceID, original.InstanceID)
	}
	if restored.WorkflowType != original.WorkflowType {
		t.Errorf("WorkflowType: got %q want %q", restored.WorkflowType, original.WorkflowType)
	}
	if restored.WorkflowVersion != original.WorkflowVersion {
		t.Errorf("WorkflowVersion: got %d want %d", restored.WorkflowVersion, original.WorkflowVersion)
	}
	if restored.AggregateType != original.AggregateType {
		t.Errorf("AggregateType: got %q want %q", restored.AggregateType, original.AggregateType)
	}
	if restored.AggregateID != original.AggregateID {
		t.Errorf("AggregateID: got %q want %q", restored.AggregateID, original.AggregateID)
	}
	if restored.CurrentState != original.CurrentState {
		t.Errorf("CurrentState: got %q want %q", restored.CurrentState, original.CurrentState)
	}
	if !reflect.DeepEqual(restored.StateData, original.StateData) {
		t.Errorf("StateData: got %v want %v", restored.StateData, original.StateData)
	}
	if restored.CorrelationID != original.CorrelationID {
		t.Errorf("CorrelationID: got %q want %q", restored.CorrelationID, original.CorrelationID)
	}
	if restored.IsStuck != original.IsStuck {
		t.Errorf("IsStuck: got %v want %v", restored.IsStuck, original.IsStuck)
	}
	if restored.StuckReason != original.StuckReason {
		t.Errorf("StuckReason: got %q want %q", restored.StuckReason, original.StuckReason)
	}
	if restored.StuckAt == nil || !restored.StuckAt.Equal(*original.StuckAt) {
		t.Errorf("StuckAt: got %v want %v", restored.StuckAt, original.StuckAt)
	}
	if restored.RetryCount != original.RetryCount {
		t.Errorf("RetryCount: got %d want %d", restored.RetryCount, original.RetryCount)
	}
	if !reflect.DeepEqual(restored.ShallowHistory, original.ShallowHistory) {
		t.Errorf("ShallowHistory: got %v want %v", restored.ShallowHistory, original.ShallowHistory)
	}
	if !reflect.DeepEqual(restored.DeepHistory, original.DeepHistory) {
		t.Errorf("DeepHistory: got %v want %v", restored.DeepHistory, original.DeepHistory)
	}
	if !reflect.DeepEqual(restored.ActiveInParallel, original.ActiveInParallel) {
		t.Errorf("ActiveInParallel: got %v want %v", restored.ActiveInParallel, original.ActiveInParallel)
	}
	if restored.ParallelClock != original.ParallelClock {
		t.Errorf("ParallelClock: got %d want %d", restored.ParallelClock, original.ParallelClock)
	}
	if !restored.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("CreatedAt: got %v want %v", restored.CreatedAt, original.CreatedAt)
	}
	if restored.DefinitionHash != original.DefinitionHash {
		t.Errorf("DefinitionHash: got %q want %q", restored.DefinitionHash, original.DefinitionHash)
	}
	if !restored.CapturedAt.Equal(original.CapturedAt) {
		t.Errorf("CapturedAt: got %v want %v", restored.CapturedAt, original.CapturedAt)
	}
}

// TestSnapshotNilMapsRoundTrip verifies that nil map fields in a Snapshot
// survive JSON serialization correctly (nil active_in_parallel omitted).
func TestSnapshotNilMapsRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 12, 10, 0, 0, 0, time.UTC)
	snap := types.Snapshot{
		InstanceID:       "inst-1",
		WorkflowType:     "simple",
		WorkflowVersion:  1,
		AggregateType:    "Order",
		AggregateID:      "o-1",
		CurrentState:     "active",
		StateData:        map[string]any{},
		ShallowHistory:   map[string]string{},
		DeepHistory:      map[string]string{},
		ActiveInParallel: nil,
		DefinitionHash:   "hash",
		CapturedAt:       now,
		CreatedAt:        now,
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var out types.Snapshot
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// ActiveInParallel with omitempty: nil stays nil after round-trip.
	if out.ActiveInParallel != nil {
		t.Errorf("ActiveInParallel: expected nil, got %v", out.ActiveInParallel)
	}
	// StuckAt nil: should remain nil.
	if out.StuckAt != nil {
		t.Errorf("StuckAt: expected nil, got %v", out.StuckAt)
	}
}
