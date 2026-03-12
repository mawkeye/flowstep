package runtime

import (
	"context"
	"errors"
	"fmt"

	"github.com/mawkeye/flowstep/types"
)

// Snapshot captures the complete runtime state of a workflow instance as a types.Snapshot.
// It is a read-only operation — no transaction is required.
// Returns ErrInstanceNotFound if no instance exists for the given aggregate.
// Returns an error if the registered compiled machine for the instance's version cannot be found.
func (e *Engine) Snapshot(ctx context.Context, aggregateType, aggregateID string) (*types.Snapshot, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: snapshot get instance: %w", err)
	}

	cm, ok := e.compiledFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstep: snapshot: no compiled machine for aggregate type %q version %d",
			aggregateType, instance.WorkflowVersion)
	}

	snap := &types.Snapshot{
		InstanceID:      instance.ID,
		WorkflowType:    instance.WorkflowType,
		WorkflowVersion: instance.WorkflowVersion,
		AggregateType:   instance.AggregateType,
		AggregateID:     instance.AggregateID,
		CurrentState:    instance.CurrentState,
		StateData:       copyMap(instance.StateData),
		CorrelationID:   instance.CorrelationID,
		IsStuck:         instance.IsStuck,
		StuckReason:     instance.StuckReason,
		StuckAt:         copyTimePtr(instance.StuckAt),
		RetryCount:      instance.RetryCount,
		CreatedAt:       instance.CreatedAt,
		ShallowHistory:  copyStringMap(instance.ShallowHistory),
		DeepHistory:     copyStringMap(instance.DeepHistory),
		ActiveInParallel: copyStringMap(instance.ActiveInParallel),
		ParallelClock:   instance.ParallelClock,
		DefinitionHash:  cm.DefinitionHash,
		CapturedAt:      e.deps.Clock.Now(),
	}

	// Normalize nil maps to empty maps for ShallowHistory and DeepHistory
	// (always present in snapshot, unlike ActiveInParallel which uses omitempty).
	if snap.ShallowHistory == nil {
		snap.ShallowHistory = make(map[string]string)
	}
	if snap.DeepHistory == nil {
		snap.DeepHistory = make(map[string]string)
	}

	return snap, nil
}

// copyStringMap deep-copies a map[string]string. Returns nil if src is nil.
func copyStringMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Restore creates a new workflow instance from a Snapshot.
//
// Validation sequence (fail-fast):
//  1. The snapshot's WorkflowVersion must be registered (compiledFor).
//  2. The snapshot's DefinitionHash must match the compiled machine's hash exactly.
//     A mismatch returns ErrSnapshotDefinitionMismatch.
//  3. No instance may already exist for (AggregateType, AggregateID).
//     If one exists, returns ErrSnapshotInstanceExists.
//
// On success, a new WorkflowInstance is created from the snapshot fields:
//   - CreatedAt is preserved from the snapshot (original instance creation time).
//   - UpdatedAt is set to the current clock time (restoration time).
//   - LastReadUpdatedAt is zero (new instance, no prior optimistic-lock read).
//
// A SnapshotRestored domain event is persisted and emitted via EventBus (if configured).
//
// NOTE: The existence check (Get-then-Create) has a TOCTOU race window under concurrent
// access. This is best-effort; production stores should enforce uniqueness via a database
// UNIQUE constraint on (aggregate_type, aggregate_id).
func (e *Engine) Restore(ctx context.Context, snap types.Snapshot) error {
	if err := e.checkShutdown(); err != nil {
		return err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	// Step 1: Resolve the compiled machine for the snapshot's version.
	cm, ok := e.compiledFor(snap.AggregateType, snap.WorkflowVersion)
	if !ok {
		return fmt.Errorf("flowstep: restore: no workflow version %d registered for aggregate type %q: %w",
			snap.WorkflowVersion, snap.AggregateType, e.deps.ErrSnapshotDefinitionMismatch)
	}

	// Step 2: Validate DefinitionHash.
	if snap.DefinitionHash != cm.DefinitionHash {
		return fmt.Errorf("flowstep: restore: definition hash mismatch (snapshot=%q registered=%q): %w",
			snap.DefinitionHash, cm.DefinitionHash, e.deps.ErrSnapshotDefinitionMismatch)
	}

	// Step 3: Reject if instance already exists.
	_, err := e.deps.InstanceStore.Get(ctx, snap.AggregateType, snap.AggregateID)
	if err == nil {
		// Instance found — reject.
		return fmt.Errorf("flowstep: restore: instance already exists for %s/%s: %w",
			snap.AggregateType, snap.AggregateID, e.deps.ErrSnapshotInstanceExists)
	}
	if !errors.Is(err, e.deps.ErrInstanceNotFound) {
		// Unexpected store error — propagate as-is.
		return fmt.Errorf("flowstep: restore: check existing instance: %w", err)
	}

	// Build the new WorkflowInstance from the snapshot.
	now := e.deps.Clock.Now()
	instance := types.WorkflowInstance{
		ID:               snap.InstanceID,
		WorkflowType:     snap.WorkflowType,
		WorkflowVersion:  snap.WorkflowVersion,
		AggregateType:    snap.AggregateType,
		AggregateID:      snap.AggregateID,
		CurrentState:     snap.CurrentState,
		StateData:        copyMap(snap.StateData),
		CorrelationID:    snap.CorrelationID,
		IsStuck:          snap.IsStuck,
		StuckReason:      snap.StuckReason,
		StuckAt:          copyTimePtr(snap.StuckAt),
		RetryCount:       snap.RetryCount,
		CreatedAt:        snap.CreatedAt,
		UpdatedAt:        now,
		ShallowHistory:   copyStringMap(snap.ShallowHistory),
		DeepHistory:      copyStringMap(snap.DeepHistory),
		ActiveInParallel: copyStringMap(snap.ActiveInParallel),
		ParallelClock:    snap.ParallelClock,
		// LastReadUpdatedAt: zero value — new instance, no prior optimistic-lock read
	}
	if instance.StateData == nil {
		instance.StateData = make(map[string]any)
	}
	if instance.ShallowHistory == nil {
		instance.ShallowHistory = make(map[string]string)
	}
	if instance.DeepHistory == nil {
		instance.DeepHistory = make(map[string]string)
	}

	// Build SnapshotRestored event.
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   snap.AggregateType,
		AggregateID:     snap.AggregateID,
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		EventType:       "SnapshotRestored",
		CorrelationID:   snap.CorrelationID,
		TransitionName:  "_snapshot_restore",
		StateBefore:     copyMap(snap.StateData),
		StateAfter:      copyMap(snap.StateData),
		Payload: map[string]any{
			"_snapshot_captured_at": snap.CapturedAt,
			"_snapshot_instance_id": snap.InstanceID,
		},
		CreatedAt: now,
	}

	// Persist instance + event in a transaction.
	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return fmt.Errorf("flowstep: restore: begin tx: %w", err)
	}
	if err := e.deps.InstanceStore.Create(ctx, tx, instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return fmt.Errorf("flowstep: restore: create instance: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return fmt.Errorf("flowstep: restore: append event: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return fmt.Errorf("flowstep: restore: commit tx: %w", err)
	}

	// Post-commit: emit via EventBus (best-effort, non-fatal).
	if e.deps.EventBus != nil {
		if emitErr := e.deps.EventBus.Emit(ctx, event); emitErr != nil {
			e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{
				Operation: "EventBus.Emit(SnapshotRestored)",
				Err:       emitErr,
			})
		}
	}

	return nil
}

