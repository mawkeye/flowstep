package pgxstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate/adapters/pgxstore"
	"github.com/mawkeye/flowstate/types"
)

func TestTaskStore_ListPendingAndExpired(t *testing.T) {
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
	if _, err := pool.Exec(ctx, "TRUNCATE flowstate_tasks"); err != nil {
		t.Fatalf("failed to truncate tasks: %v", err)
	}

	store := pgxstore.NewTaskStore(pool)
	now := time.Now().UTC()

	// 1. Pending task, future expiration (not expired)
	task1 := types.PendingTask{
		ID:            "t1",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "corr1",
		TaskType:      "approval",
		Description:   "Task 1",
		Options:       []string{"approve", "reject"},
		Status:        "PENDING",
		CreatedAt:     now,
		Timeout:       time.Hour,
		ExpiresAt:     now.Add(time.Hour),
	}

	// 2. Pending task, past expiration (expired)
	task2 := types.PendingTask{
		ID:            "t2",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "corr2",
		TaskType:      "approval",
		Description:   "Task 2",
		Options:       []string{"approve", "reject"},
		Status:        "PENDING",
		CreatedAt:     now.Add(-2 * time.Hour),
		Timeout:       time.Hour,
		ExpiresAt:     now.Add(-time.Hour),
	}

	// 3. Completed task (should be ignored by both)
	task3 := types.PendingTask{
		ID:            "t3",
		WorkflowType:  "wf",
		AggregateType: "agg",
		AggregateID:   "agg1",
		CorrelationID: "corr3",
		TaskType:      "approval",
		Description:   "Task 3",
		Options:       []string{"approve", "reject"},
		Status:        "COMPLETED",
		CreatedAt:     now,
	}

	if err := store.Create(ctx, nil, task1); err != nil {
		t.Fatalf("failed to create task1: %v", err)
	}
	if err := store.Create(ctx, nil, task2); err != nil {
		t.Fatalf("failed to create task2: %v", err)
	}
	if err := store.Create(ctx, nil, task3); err != nil {
		t.Fatalf("failed to create task3: %v", err)
	}

	t.Run("ListPending", func(t *testing.T) {
		tasks, err := store.ListPending(ctx)
		if err != nil {
			t.Fatalf("ListPending failed: %v", err)
		}

		// Should return t1 and t2
		if len(tasks) != 2 {
			t.Errorf("expected 2 pending tasks, got %d", len(tasks))
		}

		ids := make(map[string]bool)
		for _, task := range tasks {
			ids[task.ID] = true
		}
		if !ids["t1"] {
			t.Error("expected t1 in pending list")
		}
		if !ids["t2"] {
			t.Error("expected t2 in pending list")
		}
	})

	t.Run("ListExpired", func(t *testing.T) {
		tasks, err := store.ListExpired(ctx)
		if err != nil {
			t.Fatalf("ListExpired failed: %v", err)
		}

		// Should return only t2
		if len(tasks) != 1 {
			t.Errorf("expected 1 expired task, got %d", len(tasks))
		}
		if len(tasks) > 0 && tasks[0].ID != "t2" {
			t.Errorf("expected expired task t2, got %s", tasks[0].ID)
		}
	})
}

func TestTaskStore_Lifecycle(t *testing.T) {
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

	store := pgxstore.NewTaskStore(pool)
	task := types.PendingTask{
		ID:            "lifecycle_task",
		WorkflowType:  "wf",
		AggregateType: "agg_lifecycle",
		AggregateID:   "agg1",
		CorrelationID: "corr",
		TaskType:      "decision",
		Status:        "PENDING",
		CreatedAt:     time.Now().UTC(),
	}

	if err := store.Create(ctx, nil, task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get(ctx, "lifecycle_task")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, got.ID)
	}

	if err := store.Complete(ctx, nil, "lifecycle_task", "approve", "user1"); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	got2, _ := store.Get(ctx, "lifecycle_task")
	if got2.Status != "COMPLETED" || got2.Choice != "approve" {
		t.Errorf("expected COMPLETED/approve, got %s/%s", got2.Status, got2.Choice)
	}
}
