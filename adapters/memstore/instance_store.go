package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// InstanceStore is an in-memory implementation of flowstate.InstanceStore.
type InstanceStore struct {
	mu        sync.RWMutex
	instances map[string]types.WorkflowInstance // key: aggregateType:aggregateID
}

// NewInstanceStore creates a new in-memory InstanceStore.
func NewInstanceStore() *InstanceStore {
	return &InstanceStore{
		instances: make(map[string]types.WorkflowInstance),
	}
}

func key(aggregateType, aggregateID string) string {
	return fmt.Sprintf("%s:%s", aggregateType, aggregateID)
}

func (s *InstanceStore) Get(_ context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inst, ok := s.instances[key(aggregateType, aggregateID)]
	if !ok {
		return nil, flowstate.ErrInstanceNotFound
	}
	copy := inst
	return &copy, nil
}

func (s *InstanceStore) Create(_ context.Context, _ any, instance types.WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[key(instance.AggregateType, instance.AggregateID)] = instance
	return nil
}

// Update persists instance changes with optimistic locking.
// The caller must pass the instance with UpdatedAt set to the NEW timestamp.
// The store compares the STORED UpdatedAt with the OLD value.
// Convention: caller reads instance, modifies it, sets a new UpdatedAt, then calls Update.
// The store needs to know the old UpdatedAt to compare — we store it and compare
// against what was last written. The engine must set UpdatedAt to a new value
// before calling Update, so the stored value != new value after success.
//
// Simplified approach: the store compares stored.UpdatedAt with the passed lastReadAt.
// We use a wrapper to pass the old timestamp via the tx parameter.
func (s *InstanceStore) Update(_ context.Context, tx any, instance types.WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(instance.AggregateType, instance.AggregateID)
	existing, ok := s.instances[k]
	if !ok {
		return flowstate.ErrInstanceNotFound
	}

	// tx carries the lastReadAt timestamp for optimistic locking.
	// If tx is an OptimisticLock, use it. Otherwise skip locking (for simple cases).
	if lock, ok := tx.(*OptimisticLock); ok {
		if !existing.UpdatedAt.Equal(lock.LastReadAt) {
			return flowstate.ErrConcurrentModification
		}
	}

	s.instances[k] = instance
	return nil
}

// OptimisticLock carries the last-read timestamp for optimistic locking.
type OptimisticLock struct {
	LastReadAt time.Time
}

func (s *InstanceStore) ListStuck(_ context.Context) ([]types.WorkflowInstance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.WorkflowInstance
	for _, inst := range s.instances {
		if inst.IsStuck {
			result = append(result, inst)
		}
	}
	return result, nil
}
