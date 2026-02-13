package memstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
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
	if !errors.Is(err, flowstate.ErrInstanceNotFound) {
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

	// First update with correct lock succeeds
	lock := &OptimisticLock{LastReadAt: now}
	inst.CurrentState = "PAID"
	inst.UpdatedAt = now.Add(time.Second)
	if err := store.Update(ctx, lock, inst); err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	// Second update with stale lock fails (stored UpdatedAt is now+1s, but lock says now)
	staleLock := &OptimisticLock{LastReadAt: now}
	inst.CurrentState = "SHIPPED"
	inst.UpdatedAt = now.Add(2 * time.Second)
	err := store.Update(ctx, staleLock, inst)
	if !errors.Is(err, flowstate.ErrConcurrentModification) {
		t.Errorf("expected ErrConcurrentModification, got %v", err)
	}

	// Update with correct lock succeeds
	correctLock := &OptimisticLock{LastReadAt: now.Add(time.Second)}
	inst.CurrentState = "SHIPPED"
	inst.UpdatedAt = now.Add(2 * time.Second)
	if err := store.Update(ctx, correctLock, inst); err != nil {
		t.Fatalf("update with correct lock failed: %v", err)
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
