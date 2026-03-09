package memstore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
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
	if !errors.Is(err, flowstate.ErrTaskNotFound) {
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
	if !errors.Is(err, flowstate.ErrTaskAlreadyCompleted) {
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
