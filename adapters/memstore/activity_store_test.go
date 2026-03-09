package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstate/types"
)

func newInvocation(id, aggType, aggID string, status string) types.ActivityInvocation {
	return types.ActivityInvocation{
		ID:            id,
		ActivityName:  "send-email",
		WorkflowType:  "wf",
		AggregateType: aggType,
		AggregateID:   aggID,
		Status:        status,
		ScheduledAt:   time.Now(),
	}
}

func TestActivityStoreCreateAndGet(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()
	inv := newInvocation("inv-1", "order", "o-1", types.ActivityStatusScheduled)

	if err := s.Create(ctx, nil, inv); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.Get(ctx, "inv-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ActivityName != "send-email" {
		t.Errorf("expected send-email, got %s", got.ActivityName)
	}
}

func TestActivityStoreGetNotFound(t *testing.T) {
	s := NewActivityStore()
	_, err := s.Get(context.Background(), "nope")
	if err == nil {
		t.Error("expected error for missing invocation, got nil")
	}
}

func TestActivityStoreUpdateStatus(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newInvocation("inv-1", "order", "o-1", types.ActivityStatusScheduled))

	result := &types.ActivityResult{Output: map[string]any{"sent": true}}
	if err := s.UpdateStatus(ctx, "inv-1", types.ActivityStatusCompleted, result); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, _ := s.Get(ctx, "inv-1")
	if got.Status != types.ActivityStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.Result == nil {
		t.Error("expected result to be set")
	}
}

func TestActivityStoreListByAggregate(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newInvocation("inv-1", "order", "o-1", types.ActivityStatusScheduled))
	_ = s.Create(ctx, nil, newInvocation("inv-2", "order", "o-1", types.ActivityStatusCompleted))
	_ = s.Create(ctx, nil, newInvocation("inv-3", "order", "o-2", types.ActivityStatusScheduled))

	list, err := s.ListByAggregate(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("list by aggregate: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 invocations for o-1, got %d", len(list))
	}
}

func TestActivityStoreListPending(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newInvocation("inv-1", "order", "o-1", types.ActivityStatusScheduled))
	_ = s.Create(ctx, nil, newInvocation("inv-2", "order", "o-2", types.ActivityStatusCompleted))

	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "inv-1" {
		t.Errorf("expected 1 scheduled invocation, got %d", len(pending))
	}
}

func TestActivityStoreListFailed(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newInvocation("inv-1", "order", "o-1", types.ActivityStatusFailed))
	_ = s.Create(ctx, nil, newInvocation("inv-2", "order", "o-2", types.ActivityStatusScheduled))

	failed, err := s.ListFailed(ctx)
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(failed) != 1 || failed[0].ID != "inv-1" {
		t.Errorf("expected 1 failed invocation, got %d", len(failed))
	}
}

func TestActivityStoreListRetryable(t *testing.T) {
	s := NewActivityStore()
	ctx := context.Background()

	retryable := newInvocation("inv-1", "order", "o-1", types.ActivityStatusFailed)
	retryable.MaxAttempts = 3
	retryable.Attempts = 1
	retryable.RetryPolicy = &types.RetryPolicy{MaxAttempts: 3}

	exhausted := newInvocation("inv-2", "order", "o-2", types.ActivityStatusFailed)
	exhausted.MaxAttempts = 1
	exhausted.Attempts = 1
	exhausted.RetryPolicy = &types.RetryPolicy{MaxAttempts: 1}

	_ = s.Create(ctx, nil, retryable)
	_ = s.Create(ctx, nil, exhausted)

	list, err := s.ListRetryable(ctx)
	if err != nil {
		t.Fatalf("list retryable: %v", err)
	}
	if len(list) != 1 || list[0].ID != "inv-1" {
		t.Errorf("expected 1 retryable invocation, got %d", len(list))
	}
}
