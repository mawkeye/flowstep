package memstore

import (
	"context"
	"sync"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// TaskStore is an in-memory implementation of flowstep.TaskStore.
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
		return nil, flowstep.ErrTaskNotFound
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
		return flowstep.ErrTaskNotFound
	}
	if task.Status == types.TaskStatusCompleted {
		return flowstep.ErrTaskAlreadyCompleted
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

// InvalidateByStates cancels all pending tasks for the given aggregate whose State is in the
// provided list. Completed tasks are not touched. Implements types.TaskInvalidator.
func (s *TaskStore) InvalidateByStates(_ context.Context, _ any, aggregateType, aggregateID string, states []string) error {
	if len(states) == 0 {
		return nil
	}
	stateSet := make(map[string]bool, len(states))
	for _, st := range states {
		stateSet[st] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.tasks {
		if t.AggregateType != aggregateType || t.AggregateID != aggregateID {
			continue
		}
		if t.Status != types.TaskStatusPending {
			continue
		}
		if stateSet[t.State] {
			t.Status = types.TaskStatusCancelled
			s.tasks[id] = t
		}
	}
	return nil
}

func (s *TaskStore) ListExpired(_ context.Context) ([]types.PendingTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// Uses the wall clock (time.Now) because the store interface does not accept
	// a timestamp parameter. Expiry checks will use wall time even with a mock clock.
	now := time.Now()
	var result []types.PendingTask
	for _, t := range s.tasks {
		if t.Status == types.TaskStatusPending && !t.ExpiresAt.IsZero() && now.After(t.ExpiresAt) {
			result = append(result, t)
		}
	}
	return result, nil
}
