package memstore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mawkeye/flowstate/types"
)


// ChildStore is an in-memory implementation of flowstate.ChildStore.
type ChildStore struct {
	mu       sync.RWMutex
	children []types.ChildRelation
}

// NewChildStore creates a new in-memory ChildStore.
func NewChildStore() *ChildStore {
	return &ChildStore{}
}

func (s *ChildStore) Create(_ context.Context, _ any, relation types.ChildRelation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.children = append(s.children, relation)
	return nil
}

func (s *ChildStore) GetByChild(_ context.Context, childAggregateType, childAggregateID string) (*types.ChildRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.children {
		if c.ChildAggregateType == childAggregateType && c.ChildAggregateID == childAggregateID {
			copy := c
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("flowstate: child relation not found")
}

func (s *ChildStore) GetByParent(_ context.Context, parentAggregateType, parentAggregateID string) ([]types.ChildRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ChildRelation
	for _, c := range s.children {
		if c.ParentAggregateType == parentAggregateType && c.ParentAggregateID == parentAggregateID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *ChildStore) GetByGroup(_ context.Context, groupID string) ([]types.ChildRelation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ChildRelation
	for _, c := range s.children {
		if c.GroupID == groupID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (s *ChildStore) Complete(_ context.Context, _ any, childAggregateType, childAggregateID, terminalState string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.children {
		if c.ChildAggregateType == childAggregateType && c.ChildAggregateID == childAggregateID {
			// CompletedAt uses the wall clock (time.Now) because the store interface
			// does not accept a timestamp parameter. For production use, prefer a store
			// backed by a database that records the commit time server-side.
			now := time.Now()
			s.children[i].Status = types.ChildStatusCompleted
			s.children[i].ChildTerminalState = terminalState
			s.children[i].CompletedAt = &now
			return nil
		}
	}
	return fmt.Errorf("flowstate: child relation not found")
}
