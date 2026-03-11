package memstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

func newTask(id, aggType, aggID, taskType string) types.PendingTask {
	now := time.Now()
	return types.PendingTask{
		ID:            id,
		WorkflowType:  "wf",
		AggregateType: aggType,
		AggregateID:   aggID,
		TaskType:      taskType,
		Status:        types.TaskStatusPending,
		CreatedAt:     now,
	}
}

func TestTaskStoreCreateAndGet(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	task := newTask("t-1", "order", "o-1", "review")

	if err := s.Create(ctx, nil, task); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := s.Get(ctx, "t-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TaskType != "review" {
		t.Errorf("expected review, got %s", got.TaskType)
	}
}

func TestTaskStoreGetNotFound(t *testing.T) {
	s := NewTaskStore()
	_, err := s.Get(context.Background(), "nope")
	if !errors.Is(err, flowstep.ErrTaskNotFound) {
		t.Errorf("expected ErrTaskNotFound, got %v", err)
	}
}

func TestTaskStoreGetByAggregate(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTask("t-1", "order", "o-1", "review"))
	_ = s.Create(ctx, nil, newTask("t-2", "order", "o-1", "approve"))
	_ = s.Create(ctx, nil, newTask("t-3", "order", "o-2", "review"))

	tasks, err := s.GetByAggregate(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("get by aggregate: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks for o-1, got %d", len(tasks))
	}
}

func TestTaskStoreComplete(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTask("t-1", "order", "o-1", "review"))

	if err := s.Complete(ctx, nil, "t-1", "approve", "user-1"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	got, _ := s.Get(ctx, "t-1")
	if got.Status != types.TaskStatusCompleted {
		t.Errorf("expected completed, got %s", got.Status)
	}
	if got.Choice != "approve" {
		t.Errorf("expected choice=approve, got %s", got.Choice)
	}
	if got.CompletedBy != "user-1" {
		t.Errorf("expected completedBy=user-1, got %s", got.CompletedBy)
	}
}

func TestTaskStoreCompleteAlreadyCompleted(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTask("t-1", "order", "o-1", "review"))
	_ = s.Complete(ctx, nil, "t-1", "approve", "user-1")

	err := s.Complete(ctx, nil, "t-1", "approve", "user-1")
	if !errors.Is(err, flowstep.ErrTaskAlreadyCompleted) {
		t.Errorf("expected ErrTaskAlreadyCompleted, got %v", err)
	}
}

func TestTaskStoreListPending(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTask("t-1", "order", "o-1", "review"))
	_ = s.Create(ctx, nil, newTask("t-2", "order", "o-2", "review"))
	_ = s.Complete(ctx, nil, "t-2", "approve", "user-1")

	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != "t-1" {
		t.Errorf("expected 1 pending task (t-1), got %d", len(pending))
	}
}

// ─── TaskInvalidator tests ────────────────────────────────────────────────────

func newTaskInState(id, aggType, aggID, state string) types.PendingTask {
	return types.PendingTask{
		ID:            id,
		WorkflowType:  "wf",
		AggregateType: aggType,
		AggregateID:   aggID,
		State:         state,
		Status:        types.TaskStatusPending,
		CreatedAt:     time.Now(),
	}
}

func TestInvalidateByStates_InvalidatesMatchingStates(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTaskInState("t1", "order", "o1", "VALIDATING"))
	_ = s.Create(ctx, nil, newTaskInState("t2", "order", "o1", "APPROVED"))

	if err := s.InvalidateByStates(ctx, nil, "order", "o1", []string{"VALIDATING"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got1, _ := s.Get(ctx, "t1")
	if got1.Status != types.TaskStatusCancelled {
		t.Errorf("t1: expected CANCELLED, got %s", got1.Status)
	}
	got2, _ := s.Get(ctx, "t2")
	if got2.Status != types.TaskStatusPending {
		t.Errorf("t2: expected PENDING (untouched), got %s", got2.Status)
	}
}

func TestInvalidateByStates_PreservesNonMatchingStates(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTaskInState("t1", "order", "o1", "APPROVED"))

	if err := s.InvalidateByStates(ctx, nil, "order", "o1", []string{"VALIDATING", "PROCESSING"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := s.Get(ctx, "t1")
	if got.Status != types.TaskStatusPending {
		t.Errorf("expected PENDING, got %s", got.Status)
	}
}

func TestInvalidateByStates_SkipsCompletedTasks(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	t1 := newTaskInState("t1", "order", "o1", "VALIDATING")
	t1.Status = types.TaskStatusCompleted
	_ = s.Create(ctx, nil, t1)

	if err := s.InvalidateByStates(ctx, nil, "order", "o1", []string{"VALIDATING"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := s.Get(ctx, "t1")
	// Completed tasks should not be overwritten to CANCELLED.
	if got.Status != types.TaskStatusCompleted {
		t.Errorf("expected COMPLETED (untouched), got %s", got.Status)
	}
}

func TestInvalidateByStates_EmptyStateList_DoesNothing(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()
	_ = s.Create(ctx, nil, newTaskInState("t1", "order", "o1", "VALIDATING"))

	if err := s.InvalidateByStates(ctx, nil, "order", "o1", []string{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := s.Get(ctx, "t1")
	if got.Status != types.TaskStatusPending {
		t.Errorf("expected PENDING, got %s", got.Status)
	}
}

func TestTaskStoreListExpired(t *testing.T) {
	s := NewTaskStore()
	ctx := context.Background()

	expired := newTask("t-exp", "order", "o-1", "review")
	expired.ExpiresAt = time.Now().Add(-time.Minute) // already expired
	notExpired := newTask("t-ok", "order", "o-2", "review")
	notExpired.ExpiresAt = time.Now().Add(time.Hour)

	_ = s.Create(ctx, nil, expired)
	_ = s.Create(ctx, nil, notExpired)

	list, err := s.ListExpired(ctx)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(list) != 1 || list[0].ID != "t-exp" {
		t.Errorf("expected 1 expired task, got %d", len(list))
	}
}
