package memstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

func TestInstanceStoreCreateAndGet(t *testing.T) {
	store := NewInstanceStore()
	ctx := context.Background()

	inst := types.WorkflowInstance{
		ID:            "wf-1",
		WorkflowType:  "simple",
		AggregateType: "order",
		AggregateID:   "o-1",
		CurrentState:  "CREATED",
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := store.Create(ctx, nil, inst); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := store.Get(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.CurrentState != "CREATED" {
		t.Errorf("expected CREATED, got %s", got.CurrentState)
	}
}

func TestInstanceStoreGetNotFound(t *testing.T) {
	store := NewInstanceStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "order", "nonexistent")
	if !errors.Is(err, flowstep.ErrInstanceNotFound) {
		t.Errorf("expected ErrInstanceNotFound, got %v", err)
	}
}

func TestInstanceStoreOptimisticLocking(t *testing.T) {
	store := NewInstanceStore()
	ctx := context.Background()
	now := time.Now()

	inst := types.WorkflowInstance{
		ID:            "wf-1",
		WorkflowType:  "simple",
		AggregateType: "order",
		AggregateID:   "o-1",
		CurrentState:  "CREATED",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := store.Create(ctx, nil, inst); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Stale LastReadUpdatedAt → concurrent modification
	stale := inst
	stale.CurrentState = "PAID"
	stale.LastReadUpdatedAt = now.Add(-time.Second) // wrong — stored is `now`
	stale.UpdatedAt = now.Add(time.Second)
	if err := store.Update(ctx, nil, stale); !errors.Is(err, flowstep.ErrConcurrentModification) {
		t.Errorf("expected ErrConcurrentModification for stale read, got %v", err)
	}

	// Correct LastReadUpdatedAt → success
	good := inst
	good.CurrentState = "PAID"
	good.LastReadUpdatedAt = now // matches stored UpdatedAt
	good.UpdatedAt = now.Add(time.Second)
	if err := store.Update(ctx, nil, good); err != nil {
		t.Fatalf("update with correct LastReadUpdatedAt failed: %v", err)
	}

	// Zero LastReadUpdatedAt → bypass locking (used for brand-new instances)
	bypass := good
	bypass.CurrentState = "SHIPPED"
	bypass.LastReadUpdatedAt = time.Time{} // zero value → skip check
	bypass.UpdatedAt = now.Add(2 * time.Second)
	if err := store.Update(ctx, nil, bypass); err != nil {
		t.Fatalf("update with zero LastReadUpdatedAt (bypass) failed: %v", err)
	}
}

func TestInstanceStoreListStuck(t *testing.T) {
	store := NewInstanceStore()
	ctx := context.Background()
	now := time.Now()

	normal := types.WorkflowInstance{
		ID: "wf-1", WorkflowType: "simple", AggregateType: "order",
		AggregateID: "o-1", CurrentState: "CREATED", CreatedAt: now, UpdatedAt: now,
	}
	stuck := types.WorkflowInstance{
		ID: "wf-2", WorkflowType: "simple", AggregateType: "order",
		AggregateID: "o-2", CurrentState: "CREATED", IsStuck: true,
		StuckReason: "activity failed", CreatedAt: now, UpdatedAt: now,
	}

	_ = store.Create(ctx, nil, normal)
	_ = store.Create(ctx, nil, stuck)

	stuckList, err := store.ListStuck(ctx)
	if err != nil {
		t.Fatalf("list stuck failed: %v", err)
	}
	if len(stuckList) != 1 {
		t.Errorf("expected 1 stuck instance, got %d", len(stuckList))
	}
}
