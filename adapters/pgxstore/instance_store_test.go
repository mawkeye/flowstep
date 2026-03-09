package pgxstore_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/adapters/pgxstore"
	"github.com/mawkeye/flowstate/types"
)

func TestInstanceStore(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE flowstate_instances"); err != nil {
		t.Fatalf("failed to truncate instances: %v", err)
	}

	store := pgxstore.NewInstanceStore(pool, flowstate.ErrInstanceNotFound)
	now := time.Now().UTC()

	inst := types.WorkflowInstance{
		ID:              "inst1",
		WorkflowType:    "wf",
		WorkflowVersion: 1,
		AggregateType:   "agg",
		AggregateID:     "agg1",
		CurrentState:    "CREATED",
		StateData:       map[string]any{"foo": "bar"},
		CorrelationID:   "corr1",
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	// Test Create
	if err := store.Create(ctx, nil, inst); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test Get
	got, err := store.Get(ctx, "agg", "agg1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != inst.ID {
		t.Errorf("expected ID %s, got %s", inst.ID, got.ID)
	}
	if got.StateData["foo"] != "bar" {
		t.Errorf("expected state data foo=bar, got %v", got.StateData)
	}

	// Test Update (Success): set LastReadUpdatedAt to what was in DB, then advance UpdatedAt
	inst.LastReadUpdatedAt = now
	inst.CurrentState = "UPDATED"
	inst.UpdatedAt = now.Add(time.Second)
	if err := store.Update(ctx, nil, inst); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got2, err := store.Get(ctx, "agg", "agg1")
	if err != nil {
		t.Fatalf("Get after update failed: %v", err)
	}
	if got2.CurrentState != "UPDATED" {
		t.Errorf("expected state UPDATED, got %s", got2.CurrentState)
	}

	// Test Update (Optimistic Locking Failure): LastReadUpdatedAt is stale (points to original now, not now+1s)
	staleInst := inst
	staleInst.LastReadUpdatedAt = now // stale — DB now has updated_at = now+1s
	staleInst.UpdatedAt = now.Add(2 * time.Second)
	staleInst.CurrentState = "STALE_WRITE"

	err = store.Update(ctx, nil, staleInst)
	if !errors.Is(err, flowstate.ErrConcurrentModification) {
		t.Errorf("expected ErrConcurrentModification, got %v", err)
	}

	// Verify state was not changed
	got3, _ := store.Get(ctx, "agg", "agg1")
	if got3.CurrentState == "STALE_WRITE" {
		t.Error("optimistic locking failed: stale update was applied")
	}

	// Test Get Not Found
	_, err = store.Get(ctx, "agg", "non-existent")
	if !errors.Is(err, flowstate.ErrInstanceNotFound) {
		t.Errorf("expected ErrInstanceNotFound, got %v", err)
	}
}
