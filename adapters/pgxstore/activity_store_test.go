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

func TestActivityStore_ListMethods(t *testing.T) {
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

	// Clean up
	if _, err := pool.Exec(ctx, "TRUNCATE flowstate_activities"); err != nil {
		t.Fatalf("failed to truncate activities: %v", err)
	}

	store := pgxstore.NewActivityStore(pool, flowstate.ErrActivityNotFound)
	now := time.Now().UTC()

	// 1. Pending (freshly scheduled, no retry time set)
	act1 := types.ActivityInvocation{
		ID:            "act1",
		ActivityName:  "test-act",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "c1",
		Status:        "SCHEDULED",
		ScheduledAt:   now,
	}
	if err := store.Create(ctx, nil, act1); err != nil {
		t.Fatalf("failed to create act1: %v", err)
	}

	// 2. Failed
	act2 := types.ActivityInvocation{
		ID:            "act2",
		ActivityName:  "test-act",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "c2",
		Status:        "FAILED",
		ScheduledAt:   now,
	}
	if err := store.Create(ctx, nil, act2); err != nil {
		t.Fatalf("failed to create act2: %v", err)
	}

	// 3. Retryable (SCHEDULED + next_retry_at in past)
	act3 := types.ActivityInvocation{
		ID:            "act3",
		ActivityName:  "test-act",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "c3",
		Status:        "SCHEDULED",
		ScheduledAt:   now,
	}
	if err := store.Create(ctx, nil, act3); err != nil {
		t.Fatalf("failed to create act3: %v", err)
	}
	// Manually set next_retry_at since Create doesn't support it
	if _, err := pool.Exec(ctx, "UPDATE flowstate_activities SET next_retry_at = $1 WHERE id = $2", now.Add(-time.Hour), "act3"); err != nil {
		t.Fatalf("failed to update act3: %v", err)
	}

	// 4. Future Retry (SCHEDULED + next_retry_at in future)
	act4 := types.ActivityInvocation{
		ID:            "act4",
		ActivityName:  "test-act",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "c4",
		Status:        "SCHEDULED",
		ScheduledAt:   now,
	}
	if err := store.Create(ctx, nil, act4); err != nil {
		t.Fatalf("failed to create act4: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE flowstate_activities SET next_retry_at = $1 WHERE id = $2", now.Add(time.Hour), "act4"); err != nil {
		t.Fatalf("failed to update act4: %v", err)
	}

	t.Run("ListPending", func(t *testing.T) {
		list, err := store.ListPending(ctx)
		if err != nil {
			t.Fatalf("ListPending failed: %v", err)
		}
		// Should return act1, act3, act4 (all SCHEDULED)
		if len(list) != 3 {
			t.Errorf("expected 3 pending activities, got %d", len(list))
		}
	})

	t.Run("ListFailed", func(t *testing.T) {
		list, err := store.ListFailed(ctx)
		if err != nil {
			t.Fatalf("ListFailed failed: %v", err)
		}
		// Should return act2
		if len(list) != 1 {
			t.Errorf("expected 1 failed activity, got %d", len(list))
		}
		if len(list) > 0 && list[0].ID != "act2" {
			t.Errorf("expected act2, got %s", list[0].ID)
		}
	})

	t.Run("ListRetryable", func(t *testing.T) {
		list, err := store.ListRetryable(ctx)
		if err != nil {
			t.Fatalf("ListRetryable failed: %v", err)
		}
		// Should return only act3 (SCHEDULED + past retry time)
		if len(list) != 1 {
			t.Errorf("expected 1 retryable activity, got %d", len(list))
		}
		if len(list) > 0 && list[0].ID != "act3" {
			t.Errorf("expected act3, got %s", list[0].ID)
		}
	})
}

func TestActivityStore_Lifecycle(t *testing.T) {
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

	store := pgxstore.NewActivityStore(pool, flowstate.ErrActivityNotFound)
	inv := types.ActivityInvocation{
		ID:            "lifecycle_act",
		ActivityName:  "act",
		WorkflowType:  "wf",
		AggregateType: "agg_lifecycle",
		AggregateID:   "agg1",
		CorrelationID: "corr",
		Status:        "SCHEDULED",
		ScheduledAt:   time.Now().UTC(),
	}

	if err := store.Create(ctx, nil, inv); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get(ctx, "lifecycle_act")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != inv.ID {
		t.Errorf("expected ID %s, got %s", inv.ID, got.ID)
	}

	res := &types.ActivityResult{Output: map[string]any{"ok": true}}
	if err := store.UpdateStatus(ctx, "lifecycle_act", "COMPLETED", res); err != nil {
		t.Fatalf("UpdateStatus failed: %v", err)
	}

	got2, _ := store.Get(ctx, "lifecycle_act")
	if got2.Status != "COMPLETED" {
		t.Errorf("expected COMPLETED, got %s", got2.Status)
	}

	// Get with non-existent ID must return ErrActivityNotFound sentinel
	_, notFoundErr := store.Get(ctx, "non-existent-activity-id")
	if !errors.Is(notFoundErr, flowstate.ErrActivityNotFound) {
		t.Errorf("expected ErrActivityNotFound for missing activity, got %v", notFoundErr)
	}
}
