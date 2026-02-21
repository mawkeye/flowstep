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

func TestChildStore(t *testing.T) {
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

	if _, err := pool.Exec(ctx, "TRUNCATE flowstate_children"); err != nil {
		t.Fatalf("failed to truncate children: %v", err)
	}

	store := pgxstore.NewChildStore(pool)
	now := time.Now().UTC()

	rel := types.ChildRelation{
		ID:                  "rel1",
		GroupID:             "grp1",
		ParentWorkflowType:  "parent_wf",
		ParentAggregateType: "parent_agg",
		ParentAggregateID:   "p1",
		ChildWorkflowType:   "child_wf",
		ChildAggregateType:  "child_agg",
		ChildAggregateID:    "c1",
		CorrelationID:       "corr1",
		Status:              "RUNNING",
		JoinPolicy:          "ALL",
		CreatedAt:           now,
	}

	// Test Create
	if err := store.Create(ctx, nil, rel); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test GetByChild
	got, err := store.GetByChild(ctx, "child_agg", "c1")
	if err != nil {
		t.Fatalf("GetByChild failed: %v", err)
	}
	if got.ID != rel.ID {
		t.Errorf("expected ID %s, got %s", rel.ID, got.ID)
	}

	// Test GetByParent
	children, err := store.GetByParent(ctx, "parent_agg", "p1")
	if err != nil {
		t.Fatalf("GetByParent failed: %v", err)
	}
	if len(children) != 1 {
		t.Errorf("expected 1 child, got %d", len(children))
	}

	// Test GetByGroup
	group, err := store.GetByGroup(ctx, "grp1")
	if err != nil {
		t.Fatalf("GetByGroup failed: %v", err)
	}
	if len(group) != 1 {
		t.Errorf("expected 1 child in group, got %d", len(group))
	}

	// Test Complete
	if err := store.Complete(ctx, nil, "child_agg", "c1", "DONE"); err != nil {
		t.Fatalf("Complete failed: %v", err)
	}
}
