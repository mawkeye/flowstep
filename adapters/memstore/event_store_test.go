package memstore

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

func TestEventStoreAppendAndList(t *testing.T) {
	store := NewEventStore()
	ctx := context.Background()

	e1 := types.DomainEvent{
		ID:            "evt-1",
		AggregateType: "order",
		AggregateID:   "o-1",
		CorrelationID: "corr-1",
		EventType:     "OrderCreated",
		CreatedAt:     time.Now(),
		SequenceNum:   1,
	}
	e2 := types.DomainEvent{
		ID:            "evt-2",
		AggregateType: "order",
		AggregateID:   "o-1",
		CorrelationID: "corr-1",
		EventType:     "OrderPaid",
		CreatedAt:     time.Now(),
		SequenceNum:   2,
	}
	e3 := types.DomainEvent{
		ID:            "evt-3",
		AggregateType: "order",
		AggregateID:   "o-2",
		CorrelationID: "corr-2",
		EventType:     "OrderCreated",
		CreatedAt:     time.Now(),
		SequenceNum:   1,
	}

	for _, e := range []types.DomainEvent{e1, e2, e3} {
		if err := store.Append(ctx, nil, e); err != nil {
			t.Fatalf("append failed: %v", err)
		}
	}

	// ListByCorrelation
	events, err := store.ListByCorrelation(ctx, "corr-1")
	if err != nil {
		t.Fatalf("list by correlation failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events for corr-1, got %d", len(events))
	}

	// ListByAggregate
	events, err = store.ListByAggregate(ctx, "order", "o-1")
	if err != nil {
		t.Fatalf("list by aggregate failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events for order/o-1, got %d", len(events))
	}

	events, err = store.ListByAggregate(ctx, "order", "o-2")
	if err != nil {
		t.Fatalf("list by aggregate failed: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event for order/o-2, got %d", len(events))
	}
}
