package memstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// InstanceStore is an in-memory implementation of flowstep.InstanceStore.
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
		return nil, flowstep.ErrInstanceNotFound
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

// Update persists instance changes with optimistic locking via instance.LastReadUpdatedAt.
// The engine sets instance.LastReadUpdatedAt = instance.UpdatedAt before any mutations.
// If LastReadUpdatedAt is zero, the locking check is skipped (used for brand-new instances).
func (s *InstanceStore) Update(_ context.Context, _ any, instance types.WorkflowInstance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(instance.AggregateType, instance.AggregateID)
	existing, ok := s.instances[k]
	if !ok {
		return flowstep.ErrInstanceNotFound
	}

	// Skip locking for zero LastReadUpdatedAt (new instances not yet re-read).
	if !instance.LastReadUpdatedAt.IsZero() && !existing.UpdatedAt.Equal(instance.LastReadUpdatedAt) {
		return flowstep.ErrConcurrentModification
	}

	s.instances[k] = instance
	return nil
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
