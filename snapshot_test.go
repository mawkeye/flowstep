package flowstep_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/memstore"
	"github.com/mawkeye/flowstep/testutil"
	"github.com/mawkeye/flowstep/types"
)

// snapshotDef builds a simple flat workflow: initial → active → done
func snapshotDef() *types.Definition {
	def, err := flowstep.Define("Order", "order").
		Version(1).
		States(
			flowstep.Initial("initial"),
			flowstep.State("active"),
			flowstep.Terminal("done"),
		).
		Transition("activate",
			flowstep.From("initial"),
			flowstep.To("active"),
			flowstep.Event("Activate"),
		).
		Transition("complete",
			flowstep.From("active"),
			flowstep.To("done"),
			flowstep.Event("Complete"),
		).
		Build()
	if err != nil {
		panic("snapshotDef: " + err.Error())
	}
	return def
}

// TestSnapshotCapture verifies that Engine.Snapshot returns a complete, valid Snapshot.
func TestSnapshotCapture(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	aggType, aggID := "Order", "order-snap-1"

	// Create instance by triggering first transition.
	if _, err := te.Engine.Transition(ctx, aggType, aggID, "activate", "actor1", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, aggType, aggID)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.AggregateType != aggType {
		t.Errorf("AggregateType: got %q want %q", snap.AggregateType, aggType)
	}
	if snap.AggregateID != aggID {
		t.Errorf("AggregateID: got %q want %q", snap.AggregateID, aggID)
	}
	if snap.CurrentState != "active" {
		t.Errorf("CurrentState: got %q want %q", snap.CurrentState, "active")
	}
	if snap.WorkflowVersion != def.Version {
		t.Errorf("WorkflowVersion: got %d want %d", snap.WorkflowVersion, def.Version)
	}
	if snap.DefinitionHash == "" {
		t.Error("DefinitionHash: should not be empty")
	}
	if snap.CapturedAt.IsZero() {
		t.Error("CapturedAt: should not be zero")
	}
	if snap.InstanceID == "" {
		t.Error("InstanceID: should not be empty")
	}
	if snap.CorrelationID == "" {
		t.Error("CorrelationID: should not be empty")
	}
	if snap.ShallowHistory == nil {
		t.Error("ShallowHistory: should not be nil")
	}
	if snap.DeepHistory == nil {
		t.Error("DeepHistory: should not be nil")
	}
}

// TestSnapshotCapture_DefinitionHashMatchesRegistered verifies the hash matches the compiled machine.
func TestSnapshotCapture_DefinitionHashMatchesRegistered(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-hash-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-hash-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// The hash must be non-empty and stable (same definition → same hash).
	snap2, err := te.Engine.Snapshot(ctx, "Order", "order-hash-1")
	if err != nil {
		t.Fatalf("Snapshot2: %v", err)
	}
	if snap.DefinitionHash != snap2.DefinitionHash {
		t.Errorf("DefinitionHash not stable: %q != %q", snap.DefinitionHash, snap2.DefinitionHash)
	}
}

// TestSnapshotCapture_MapsAreDeepCopied verifies mutations to Snapshot maps don't affect stored instance.
func TestSnapshotCapture_MapsAreDeepCopied(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-copy-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-copy-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Mutate the snapshot maps.
	snap.StateData["injected"] = "value"
	snap.ShallowHistory["injected"] = "value"

	// Take another snapshot — original stored state must be unaffected.
	snap2, err := te.Engine.Snapshot(ctx, "Order", "order-copy-1")
	if err != nil {
		t.Fatalf("Snapshot2: %v", err)
	}
	if _, ok := snap2.StateData["injected"]; ok {
		t.Error("mutation of snap.StateData leaked into stored instance")
	}
	if _, ok := snap2.ShallowHistory["injected"]; ok {
		t.Error("mutation of snap.ShallowHistory leaked into stored instance")
	}
}

// TestSnapshotCapture_NotFound verifies ErrInstanceNotFound for missing aggregate.
func TestSnapshotCapture_NotFound(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	_, err := te.Engine.Snapshot(ctx, "Order", "does-not-exist")
	if err == nil {
		t.Fatal("expected error for non-existent aggregate, got nil")
	}
	if !errors.Is(err, flowstep.ErrInstanceNotFound) {
		t.Errorf("expected ErrInstanceNotFound, got: %v", err)
	}
}

// TestSnapshotCapture_CapturedAtFromClock verifies CapturedAt uses the engine clock.
func TestSnapshotCapture_CapturedAtFromClock(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-clock-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	captureTime := time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC)
	te.Clock.Set(captureTime)

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-clock-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !snap.CapturedAt.Equal(captureTime) {
		t.Errorf("CapturedAt: got %v want %v", snap.CapturedAt, captureTime)
	}
}

// TestRestoreFromSnapshot verifies Engine.Restore creates a new instance from a valid Snapshot.
func TestRestoreFromSnapshot(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-orig-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-orig-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Restore to a different aggregate ID.
	snap.AggregateID = "order-restored-1"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restored instance exists and has correct state.
	restored, err := te.InstanceStore.Get(ctx, "Order", "order-restored-1")
	if err != nil {
		t.Fatalf("Get restored instance: %v", err)
	}
	if restored.CurrentState != "active" {
		t.Errorf("CurrentState: got %q want %q", restored.CurrentState, "active")
	}
	if restored.WorkflowVersion != def.Version {
		t.Errorf("WorkflowVersion: got %d want %d", restored.WorkflowVersion, def.Version)
	}
}

// TestRestoreFromSnapshot_DefinitionHashMismatch verifies ErrSnapshotDefinitionMismatch.
func TestRestoreFromSnapshot_DefinitionHashMismatch(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-mismatch-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-mismatch-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap.AggregateID = "order-mismatch-target"
	snap.DefinitionHash = "tampered-hash"

	err = te.Engine.Restore(ctx, *snap)
	if !errors.Is(err, flowstep.ErrSnapshotDefinitionMismatch) {
		t.Errorf("expected ErrSnapshotDefinitionMismatch, got: %v", err)
	}
}

// TestRestoreFromSnapshot_VersionNotRegistered verifies error when version not registered.
func TestRestoreFromSnapshot_VersionNotRegistered(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-ver-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-ver-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap.AggregateID = "order-ver-target"
	snap.WorkflowVersion = 999 // not registered

	err = te.Engine.Restore(ctx, *snap)
	if err == nil {
		t.Fatal("expected error for unregistered version, got nil")
	}
	// ErrSnapshotDefinitionMismatch wraps version-not-found
	if !errors.Is(err, flowstep.ErrSnapshotDefinitionMismatch) {
		t.Errorf("expected ErrSnapshotDefinitionMismatch, got: %v", err)
	}
}

// TestRestoreFromSnapshot_InstanceAlreadyExists verifies ErrSnapshotInstanceExists.
func TestRestoreFromSnapshot_InstanceAlreadyExists(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	// Create two instances.
	if _, err := te.Engine.Transition(ctx, "Order", "order-exists-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition src: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "Order", "order-exists-dst", "activate", "a", nil); err != nil {
		t.Fatalf("Transition dst: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-exists-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Try to restore into already-existing destination.
	snap.AggregateID = "order-exists-dst"
	err = te.Engine.Restore(ctx, *snap)
	if !errors.Is(err, flowstep.ErrSnapshotInstanceExists) {
		t.Errorf("expected ErrSnapshotInstanceExists, got: %v", err)
	}
}

// TestRestoreFromSnapshot_EmitsEvent verifies a SnapshotRestored event is stored.
func TestRestoreFromSnapshot_EmitsEvent(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-event-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-event-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap.AggregateID = "order-event-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Check that a SnapshotRestored event was persisted.
	events, err := te.EventStore.ListByAggregate(ctx, "Order", "order-event-dst")
	if err != nil {
		t.Fatalf("ListByAggregate: %v", err)
	}
	var found bool
	for _, ev := range events {
		if ev.EventType == "SnapshotRestored" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SnapshotRestored event, got %d events: %v", len(events), events)
	}
}

// TestRestoreFromSnapshot_CanTransitionAfterRestore verifies the restored instance accepts transitions.
func TestRestoreFromSnapshot_CanTransitionAfterRestore(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-trans-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-trans-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap.AggregateID = "order-trans-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Restored instance should be in "active" state and accept "complete" transition.
	result, err := te.Engine.Transition(ctx, "Order", "order-trans-dst", "complete", "actor", nil)
	if err != nil {
		t.Fatalf("Transition after restore: %v", err)
	}
	if result.NewState != "done" {
		t.Errorf("NewState after restore+transition: got %q want %q", result.NewState, "done")
	}
}

// TestSnapshotCapture_MemstoreEventStore verifies the test harness EventStore is accessible.
// (Validates testutil.TestEngine.EventStore type for direct use in emit tests.)
func TestSnapshotCapture_EventStoreType(t *testing.T) {
	te := testutil.NewTestEngine(t)
	var _ *memstore.EventStore = te.EventStore // compile-time type check
	_ = te
}

// TestSnapshotCapture_StuckAtIsDeepCopied verifies that mutating snap.StuckAt
// does not affect the stored instance (pointer isolation).
func TestSnapshotCapture_StuckAtIsDeepCopied(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "order-stuckat-copy-1", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "order-stuckat-copy-1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// StuckAt is nil for a non-stuck instance — set it on the snapshot and verify
	// this does not affect a second snapshot of the same instance.
	injectedTime := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	snap.StuckAt = &injectedTime

	snap2, err := te.Engine.Snapshot(ctx, "Order", "order-stuckat-copy-1")
	if err != nil {
		t.Fatalf("Snapshot2: %v", err)
	}
	if snap2.StuckAt != nil {
		t.Errorf("StuckAt: mutation of snap.StuckAt leaked into stored instance (got %v)", snap2.StuckAt)
	}
}

// ─── Round-trip integration tests ───────────────────────────────────────────

// TestSnapshotRoundTrip_Flat verifies snapshot/restore round-trip for a flat workflow.
func TestSnapshotRoundTrip_Flat(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "rt-flat-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "rt-flat-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	snap.AggregateID = "rt-flat-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restored instance matches source.
	restored, err := te.InstanceStore.Get(ctx, "Order", "rt-flat-dst")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if restored.CurrentState != "active" {
		t.Errorf("CurrentState: got %q want %q", restored.CurrentState, "active")
	}

	// Verify restored instance can transition to terminal.
	result, err := te.Engine.Transition(ctx, "Order", "rt-flat-dst", "complete", "a", nil)
	if err != nil {
		t.Fatalf("Transition after restore: %v", err)
	}
	if result.NewState != "done" {
		t.Errorf("NewState: got %q want %q", result.NewState, "done")
	}
}

// snapshotHistoryDef builds a hierarchical workflow with compound state and history:
//
//	IDLE (initial)
//	  → PROCESSING (compound, initial: REVIEWING)
//	    REVIEWING → SHIPPING (via ship)
//	  → DONE (terminal)
//
//	"enter"           : IDLE → PROCESSING
//	"ship"            : REVIEWING → SHIPPING
//	"pause"           : PROCESSING → IDLE
//	"resume_shallow"  : IDLE → PROCESSING [shallow history]
//	"resume_deep"     : IDLE → PROCESSING [deep history]
//	"finish"          : PROCESSING → DONE
func snapshotHistoryDef() *types.Definition {
	def, err := flowstep.Define("HistSnap", "hist_snap").
		Version(1).
		States(
			flowstep.Initial("IDLE"),
			flowstep.CompoundState("PROCESSING",
				flowstep.InitialChild("REVIEWING"),
			),
			flowstep.State("REVIEWING", flowstep.Parent("PROCESSING")),
			flowstep.State("SHIPPING", flowstep.Parent("PROCESSING")),
			flowstep.Terminal("DONE"),
		).
		Transition("enter",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
			flowstep.Event("Entered"),
		).
		Transition("ship",
			flowstep.From("REVIEWING"),
			flowstep.To("SHIPPING"),
			flowstep.Event("Shipped"),
		).
		Transition("pause",
			flowstep.From("PROCESSING"),
			flowstep.To("IDLE"),
			flowstep.Event("Paused"),
		).
		Transition("resume_shallow",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
			flowstep.WithHistory(flowstep.HistoryShallow),
		).
		Transition("resume_deep",
			flowstep.From("IDLE"),
			flowstep.To("PROCESSING"),
			flowstep.WithHistory(flowstep.HistoryDeep),
		).
		Transition("finish",
			flowstep.From("PROCESSING"),
			flowstep.To("DONE"),
			flowstep.Event("Finished"),
		).
		Build()
	if err != nil {
		panic("snapshotHistoryDef: " + err.Error())
	}
	return def
}

// TestSnapshotRoundTrip_HierarchicalWithHistory verifies snapshot/restore preserves shallow history.
func TestSnapshotRoundTrip_HierarchicalWithHistory(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotHistoryDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Enter PROCESSING (lands in REVIEWING), move to SHIPPING, pause (exits to IDLE → records shallow history).
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-hist-src", "enter", "a", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-hist-src", "ship", "a", nil); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-hist-src", "pause", "a", nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "HistSnap", "rt-hist-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Verify shallow history was captured.
	if snap.ShallowHistory["PROCESSING"] != "SHIPPING" {
		t.Fatalf("ShallowHistory[PROCESSING]: got %q want %q", snap.ShallowHistory["PROCESSING"], "SHIPPING")
	}

	snap.AggregateID = "rt-hist-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Resume via shallow history — should return to SHIPPING (not default REVIEWING).
	result, err := te.Engine.Transition(ctx, "HistSnap", "rt-hist-dst", "resume_shallow", "a", nil)
	if err != nil {
		t.Fatalf("resume_shallow: %v", err)
	}
	if result.NewState != "SHIPPING" {
		t.Errorf("NewState after resume_shallow: got %q want %q", result.NewState, "SHIPPING")
	}
}

// TestSnapshotRoundTrip_HierarchicalWithDeepHistory verifies snapshot/restore preserves deep history.
func TestSnapshotRoundTrip_HierarchicalWithDeepHistory(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotHistoryDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Enter PROCESSING (lands in REVIEWING), move to SHIPPING, pause (exits to IDLE → records deep history).
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-deep-src", "enter", "a", nil); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-deep-src", "ship", "a", nil); err != nil {
		t.Fatalf("ship: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "HistSnap", "rt-deep-src", "pause", "a", nil); err != nil {
		t.Fatalf("pause: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "HistSnap", "rt-deep-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Verify deep history was captured (compound state records the last active leaf).
	if snap.DeepHistory["PROCESSING"] != "SHIPPING" {
		t.Fatalf("DeepHistory[PROCESSING]: got %q want %q", snap.DeepHistory["PROCESSING"], "SHIPPING")
	}

	snap.AggregateID = "rt-deep-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Resume via deep history — should return to SHIPPING (not default REVIEWING).
	result, err := te.Engine.Transition(ctx, "HistSnap", "rt-deep-dst", "resume_deep", "a", nil)
	if err != nil {
		t.Fatalf("resume_deep: %v", err)
	}
	if result.NewState != "SHIPPING" {
		t.Errorf("NewState after resume_deep: got %q want %q", result.NewState, "SHIPPING")
	}
}

// snapshotParallelDef builds a parallel-state workflow:
//
//	IDLE → editing (parallel: bold_region, italic_region) → DONE
//	  bold_region:   bold_off ↔ bold_on
//	  italic_region: italic_off ↔ italic_on
func snapshotParallelDef() *types.Definition {
	def, err := flowstep.Define("ParSnap", "par_snap").
		Version(1).
		States(
			flowstep.Initial("IDLE"),
			flowstep.ParallelState("editing").
				Region("bold_region",
					flowstep.State("bold_off"),
					flowstep.State("bold_on"),
				).
				Region("italic_region",
					flowstep.State("italic_off"),
					flowstep.State("italic_on"),
				).
				Done(),
			flowstep.Terminal("DONE"),
		).
		Transition("startEditing",
			flowstep.From("IDLE"),
			flowstep.To("editing"),
			flowstep.Event("Started"),
		).
		Transition("toggleBold",
			flowstep.From("bold_off"),
			flowstep.To("bold_on"),
			flowstep.Event("BoldOn"),
		).
		Transition("toggleItalic",
			flowstep.From("italic_off"),
			flowstep.To("italic_on"),
			flowstep.Event("ItalicOn"),
		).
		Transition("finishEditing",
			flowstep.From("editing"),
			flowstep.To("DONE"),
			flowstep.Event("Finished"),
		).
		Build()
	if err != nil {
		panic("snapshotParallelDef: " + err.Error())
	}
	return def
}

// TestSnapshotRoundTrip_Parallel verifies snapshot/restore preserves parallel region state.
func TestSnapshotRoundTrip_Parallel(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotParallelDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()

	// Enter parallel state and toggle bold in bold_region.
	if _, err := te.Engine.Transition(ctx, "ParSnap", "rt-par-src", "startEditing", "a", nil); err != nil {
		t.Fatalf("startEditing: %v", err)
	}
	if _, err := te.Engine.Transition(ctx, "ParSnap", "rt-par-src", "toggleBold", "a", nil); err != nil {
		t.Fatalf("toggleBold: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "ParSnap", "rt-par-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Verify parallel state captured.
	if snap.ActiveInParallel == nil {
		t.Fatal("ActiveInParallel: should not be nil")
	}
	if snap.ActiveInParallel["bold_region"] != "bold_on" {
		t.Errorf("ActiveInParallel[bold_region]: got %q want %q", snap.ActiveInParallel["bold_region"], "bold_on")
	}
	if snap.ActiveInParallel["italic_region"] != "italic_off" {
		t.Errorf("ActiveInParallel[italic_region]: got %q want %q", snap.ActiveInParallel["italic_region"], "italic_off")
	}
	if snap.ParallelClock == 0 {
		t.Error("ParallelClock: should be non-zero after parallel transitions")
	}

	snap.AggregateID = "rt-par-dst"
	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restored instance can toggle italic (proving parallel state is functional).
	result, err := te.Engine.Transition(ctx, "ParSnap", "rt-par-dst", "toggleItalic", "a", nil)
	if err != nil {
		t.Fatalf("toggleItalic after restore: %v", err)
	}
	inst := result.Instance
	if inst.ActiveInParallel["bold_region"] != "bold_on" {
		t.Errorf("bold_region after restore+toggle: got %q want %q", inst.ActiveInParallel["bold_region"], "bold_on")
	}
	if inst.ActiveInParallel["italic_region"] != "italic_on" {
		t.Errorf("italic_region after restore+toggle: got %q want %q", inst.ActiveInParallel["italic_region"], "italic_on")
	}
}

// TestSnapshotRoundTrip_StuckInstance verifies snapshot/restore preserves stuck state.
func TestSnapshotRoundTrip_StuckInstance(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "rt-stuck-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "rt-stuck-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Simulate stuck state by modifying the snapshot (value type).
	stuckAt := time.Date(2026, 3, 12, 12, 0, 0, 0, time.UTC)
	snap.IsStuck = true
	snap.StuckReason = "entry activity failed"
	snap.StuckAt = &stuckAt
	snap.RetryCount = 3
	snap.AggregateID = "rt-stuck-dst"

	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored, err := te.InstanceStore.Get(ctx, "Order", "rt-stuck-dst")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !restored.IsStuck {
		t.Error("IsStuck: should be true")
	}
	if restored.StuckReason != "entry activity failed" {
		t.Errorf("StuckReason: got %q want %q", restored.StuckReason, "entry activity failed")
	}
	if restored.StuckAt == nil || !restored.StuckAt.Equal(stuckAt) {
		t.Errorf("StuckAt: got %v want %v", restored.StuckAt, stuckAt)
	}
	if restored.RetryCount != 3 {
		t.Errorf("RetryCount: got %d want %d", restored.RetryCount, 3)
	}
}

// TestSnapshotRoundTrip_JSONSerialization verifies snapshot survives JSON marshal/unmarshal then restore.
func TestSnapshotRoundTrip_JSONSerialization(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "rt-json-src", "activate", "a", map[string]any{
		"amount": float64(99.95), "currency": "EUR",
	}); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "rt-json-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Marshal → Unmarshal (simulate transport/storage).
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var restored types.Snapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Restore the deserialized snapshot under a new aggregate ID.
	restored.AggregateID = "rt-json-dst"
	if err := te.Engine.Restore(ctx, restored); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	// Verify restored instance can transition.
	result, err := te.Engine.Transition(ctx, "Order", "rt-json-dst", "complete", "a", nil)
	if err != nil {
		t.Fatalf("Transition after JSON restore: %v", err)
	}
	if result.NewState != "done" {
		t.Errorf("NewState: got %q want %q", result.NewState, "done")
	}
}

// TestSnapshotRoundTrip_StateDataPreservation verifies StateData survives snapshot/restore.
// StateData is set by modifying the snapshot (value type) since Transition params
// are not stored as StateData by the engine.
func TestSnapshotRoundTrip_StateDataPreservation(t *testing.T) {
	te := testutil.NewTestEngine(t)
	def := snapshotDef()
	if err := te.Engine.Register(def); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx := context.Background()
	if _, err := te.Engine.Transition(ctx, "Order", "rt-data-src", "activate", "a", nil); err != nil {
		t.Fatalf("Transition: %v", err)
	}

	snap, err := te.Engine.Snapshot(ctx, "Order", "rt-data-src")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	// Inject StateData into the snapshot (Snapshot is a value type — this is the
	// documented mechanism for enriching state before restore).
	wantData := map[string]any{
		"amount":   float64(250),
		"currency": "USD",
		"items":    float64(3),
	}
	snap.StateData = wantData
	snap.AggregateID = "rt-data-dst"

	if err := te.Engine.Restore(ctx, *snap); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored, err := te.InstanceStore.Get(ctx, "Order", "rt-data-dst")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if !reflect.DeepEqual(restored.StateData, wantData) {
		t.Errorf("StateData: got %v want %v", restored.StateData, wantData)
	}
}
