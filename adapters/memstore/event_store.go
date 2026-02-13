package memstore

import (
	"context"
	"sync"

	"github.com/mawkeye/flowstate/types"
)

// EventStore is an in-memory implementation of flowstate.EventStore.
type EventStore struct {
	mu     sync.RWMutex
	events []types.DomainEvent
}

// NewEventStore creates a new in-memory EventStore.
func NewEventStore() *EventStore {
	return &EventStore{}
}

func (s *EventStore) Append(_ context.Context, _ any, event types.DomainEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *EventStore) ListByCorrelation(_ context.Context, correlationID string) ([]types.DomainEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.DomainEvent
	for _, e := range s.events {
		if e.CorrelationID == correlationID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (s *EventStore) ListByAggregate(_ context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []types.DomainEvent
	for _, e := range s.events {
		if e.AggregateType == aggregateType && e.AggregateID == aggregateID {
			result = append(result, e)
		}
	}
	return result, nil
}
