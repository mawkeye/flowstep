package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// TaskStore is an in-memory implementation of flowstate.TaskStore.
type TaskStore struct {
	mu    sync.RWMutex
	tasks map[string]types.PendingTask // key: task ID
}

// NewTaskStore creates a new in-memory TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: make(map[string]types.PendingTask),
	}
}

func (s *TaskStore) Create(_ context.Context, _ any, task types.PendingTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	return nil
}

func (s *TaskStore) Get(_ context.Context, taskID string) (*types.PendingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return nil, flowstate.ErrTaskNotFound
	}
	copy := task
	return &copy, nil
}

func (s *TaskStore) GetByAggregate(_ context.Context, aggregateType, aggregateID string) ([]types.PendingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.PendingTask
	for _, t := range s.tasks {
		if t.AggregateType == aggregateType && t.AggregateID == aggregateID {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *TaskStore) Complete(_ context.Context, _ any, taskID, choice, actorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[taskID]
	if !ok {
		return flowstate.ErrTaskNotFound
	}
	if task.Status == types.TaskStatusCompleted {
		return flowstate.ErrTaskAlreadyCompleted
	}
	// CompletedAt uses the wall clock (time.Now) because the store interface
	// does not accept a timestamp parameter. For production use, prefer a store
	// backed by a database that records the commit time server-side.
	now := time.Now()
	task.Status = types.TaskStatusCompleted
	task.Choice = choice
	task.CompletedBy = actorID
	task.CompletedAt = &now
	s.tasks[taskID] = task
	return nil
}

func (s *TaskStore) ListPending(_ context.Context) ([]types.PendingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.PendingTask
	for _, t := range s.tasks {
		if t.Status == types.TaskStatusPending {
			result = append(result, t)
		}
	}
	return result, nil
}

func (s *TaskStore) ListExpired(_ context.Context) ([]types.PendingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	now := time.Now()
	var result []types.PendingTask
	for _, t := range s.tasks {
		if t.Status == types.TaskStatusPending && !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt) {
			result = append(result, t)
		}
	}
	return result, nil
}
