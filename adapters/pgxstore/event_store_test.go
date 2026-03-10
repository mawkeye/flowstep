package pgxstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstep/adapters/pgxstore"
	"github.com/mawkeye/flowstep/types"
)

func TestEventStore(t *testing.T) {
	connString := os.Getenv("FLOWSTATE_TEST_DB")
	if connString == "" {
		t.Skip("Skipping pgxstore tests: FLOWSTATE_TEST_DB not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, "TRUNCATE flowstep_events"); err != nil {
		t.Fatalf("failed to truncate events: %v", err)
	}

	store := pgxstore.NewEventStore(pool)
	now := time.Now().UTC()

	event1 := types.DomainEvent{
		ID:              "evt1",
		AggregateType:   "order",
		AggregateID:     "o1",
		WorkflowType:    "order_wf",
		WorkflowVersion: 1,
		EventType:       "OrderCreated",
		CorrelationID:   "corr1",
		ActorID:         "user1",
		StateBefore:     map[string]any{"status": "none"},
		StateAfter:      map[string]any{"status": "created"},
		Payload:         map[string]any{"amount": 100},
		CreatedAt:       now,
	}

	event2 := types.DomainEvent{
		ID:              "evt2",
		AggregateType:   "order",
		AggregateID:     "o1",
		WorkflowType:    "order_wf",
		WorkflowVersion: 1,
		EventType:       "OrderPaid",
		CorrelationID:   "corr1",
		ActorID:         "user1",
		StateBefore:     map[string]any{"status": "created"},
		StateAfter:      map[string]any{"status": "paid"},
		CreatedAt:       now.Add(time.Second),
	}

	// Test Append
	if err := store.Append(ctx, nil, event1); err != nil {
		t.Fatalf("Append event1 failed: %v", err)
	}
	if err := store.Append(ctx, nil, event2); err != nil {
		t.Fatalf("Append event2 failed: %v", err)
	}

	// Test ListByCorrelation
	events, err := store.ListByCorrelation(ctx, "corr1")
	if err != nil {
		t.Fatalf("ListByCorrelation failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
	if events[0].ID != "evt1" || events[1].ID != "evt2" {
		t.Error("events returned in wrong order or content mismatch")
	}

	// Test ListByAggregate
	aggEvents, err := store.ListByAggregate(ctx, "order", "o1")
	if err != nil {
		t.Fatalf("ListByAggregate failed: %v", err)
	}
	if len(aggEvents) != 2 {
		t.Errorf("expected 2 events by aggregate, got %d", len(aggEvents))
	}
}
