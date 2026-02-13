package memstore

import (
	"context"
	"fmt"
	"sync"

	"github.com/mawkeye/flowstate/types"
)

// ActivityStore is an in-memory implementation of flowstate.ActivityStore.
type ActivityStore struct {
	mu          sync.RWMutex
	invocations map[string]types.ActivityInvocation // key: invocation ID
}

// NewActivityStore creates a new in-memory ActivityStore.
func NewActivityStore() *ActivityStore {
	return &ActivityStore{
		invocations: make(map[string]types.ActivityInvocation),
	}
}

func (s *ActivityStore) Create(_ context.Context, _ any, invocation types.ActivityInvocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invocations[invocation.ID] = invocation
	return nil
}

func (s *ActivityStore) Get(_ context.Context, invocationID string) (*types.ActivityInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	inv, ok := s.invocations[invocationID]
	if !ok {
		return nil, fmt.Errorf("flowstate: activity invocation not found")
	}
	copy := inv
	return &copy, nil
}

func (s *ActivityStore) UpdateStatus(_ context.Context, invocationID, status string, result *types.ActivityResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inv, ok := s.invocations[invocationID]
	if !ok {
		return fmt.Errorf("flowstate: activity invocation not found")
	}
	inv.Status = status
	inv.Result = result
	s.invocations[invocationID] = inv
	return nil
}

func (s *ActivityStore) ListByAggregate(_ context.Context, aggregateType, aggregateID string) ([]types.ActivityInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ActivityInvocation
	for _, inv := range s.invocations {
		if inv.AggregateType == aggregateType && inv.AggregateID == aggregateID {
			result = append(result, inv)
		}
	}
	return result, nil
}

func (s *ActivityStore) ListPending(_ context.Context) ([]types.ActivityInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ActivityInvocation
	for _, inv := range s.invocations {
		if inv.Status == types.ActivityStatusScheduled {
			result = append(result, inv)
		}
	}
	return result, nil
}

func (s *ActivityStore) ListFailed(_ context.Context) ([]types.ActivityInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ActivityInvocation
	for _, inv := range s.invocations {
		if inv.Status == types.ActivityStatusFailed {
			result = append(result, inv)
		}
	}
	return result, nil
}

func (s *ActivityStore) ListRetryable(_ context.Context) ([]types.ActivityInvocation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.ActivityInvocation
	for _, inv := range s.invocations {
		if inv.Status == types.ActivityStatusFailed && inv.RetryPolicy != nil && inv.Attempts < inv.MaxAttempts {
			result = append(result, inv)
		}
	}
	return result, nil
}
