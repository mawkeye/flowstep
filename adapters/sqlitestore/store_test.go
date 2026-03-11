package sqlitestore_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/adapters/sqlitestore"
	"github.com/mawkeye/flowstep/types"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Apply all migrations in order.
	err = fs.WalkDir(sqlitestore.Migrations, "migrations", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := sqlitestore.Migrations.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		_, execErr := db.Exec(string(data))
		return execErr
	})
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return db
}

func TestSQLiteInstanceStore_HistoryRoundTrip(t *testing.T) {
	db := openTestDB(t)
	store := sqlitestore.NewInstanceStore(db, flowstep.ErrInstanceNotFound)
	txProvider := sqlitestore.NewTxProvider(db)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	inst := types.WorkflowInstance{
		ID:              "inst-1",
		WorkflowType:    "wf",
		WorkflowVersion: 1,
		AggregateType:   "agg",
		AggregateID:     "agg-1",
		CurrentState:    "IDLE",
		StateData:       map[string]any{"key": "val"},
		CorrelationID:   "corr-1",
		ShallowHistory:  map[string]string{"PROCESSING": "SHIPPING"},
		DeepHistory:     map[string]string{"PROCESSING": "CAPTURE"},
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	tx, err := txProvider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := store.Create(ctx, tx, inst); err != nil {
		_ = txProvider.Rollback(ctx, tx)
		t.Fatalf("Create: %v", err)
	}
	if err := txProvider.Commit(ctx, tx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	got, err := store.Get(ctx, "agg", "agg-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ShallowHistory["PROCESSING"] != "SHIPPING" {
		t.Errorf("ShallowHistory[PROCESSING] = %q, want SHIPPING", got.ShallowHistory["PROCESSING"])
	}
	if got.DeepHistory["PROCESSING"] != "CAPTURE" {
		t.Errorf("DeepHistory[PROCESSING] = %q, want CAPTURE", got.DeepHistory["PROCESSING"])
	}

	// Test Update persists changed history.
	inst.LastReadUpdatedAt = now
	inst.UpdatedAt = now.Add(time.Second)
	inst.ShallowHistory = map[string]string{"PROCESSING": "REVIEWING"}
	inst.DeepHistory = map[string]string{"PROCESSING": "DRAFT"}

	tx2, err := txProvider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	if err := store.Update(ctx, tx2, inst); err != nil {
		_ = txProvider.Rollback(ctx, tx2)
		t.Fatalf("Update: %v", err)
	}
	if err := txProvider.Commit(ctx, tx2); err != nil {
		t.Fatalf("Commit tx2: %v", err)
	}

	got2, err := store.Get(ctx, "agg", "agg-1")
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if got2.ShallowHistory["PROCESSING"] != "REVIEWING" {
		t.Errorf("after Update ShallowHistory[PROCESSING] = %q, want REVIEWING", got2.ShallowHistory["PROCESSING"])
	}
	if got2.DeepHistory["PROCESSING"] != "DRAFT" {
		t.Errorf("after Update DeepHistory[PROCESSING] = %q, want DRAFT", got2.DeepHistory["PROCESSING"])
	}
}

func TestSQLiteInstanceStore_ListStuck_HasHistoryMaps(t *testing.T) {
	db := openTestDB(t)
	store := sqlitestore.NewInstanceStore(db, flowstep.ErrInstanceNotFound)
	txProvider := sqlitestore.NewTxProvider(db)

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)

	inst := types.WorkflowInstance{
		ID:             "stuck-1",
		WorkflowType:   "wf",
		AggregateType:  "agg",
		AggregateID:    "stuck-agg-1",
		CurrentState:   "STUCK_STATE",
		CorrelationID:  "corr-stuck",
		IsStuck:        true,
		ShallowHistory: map[string]string{"ROOT": "CHILD"},
		DeepHistory:    map[string]string{"ROOT": "LEAF"},
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	tx, err := txProvider.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := store.Create(ctx, tx, inst); err != nil {
		_ = txProvider.Rollback(ctx, tx)
		t.Fatalf("Create: %v", err)
	}
	if err := txProvider.Commit(ctx, tx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	stuckList, err := store.ListStuck(ctx)
	if err != nil {
		t.Fatalf("ListStuck: %v", err)
	}
	if len(stuckList) != 1 {
		t.Fatalf("expected 1 stuck instance, got %d", len(stuckList))
	}
	if stuckList[0].ShallowHistory["ROOT"] != "CHILD" {
		t.Errorf("ListStuck ShallowHistory[ROOT] = %q, want CHILD", stuckList[0].ShallowHistory["ROOT"])
	}
	if stuckList[0].DeepHistory["ROOT"] != "LEAF" {
		t.Errorf("ListStuck DeepHistory[ROOT] = %q, want LEAF", stuckList[0].DeepHistory["ROOT"])
	}
}

func TestSQLiteInstanceStore_GetNotFound(t *testing.T) {
	db := openTestDB(t)
	store := sqlitestore.NewInstanceStore(db, flowstep.ErrInstanceNotFound)

	_, err := store.Get(context.Background(), "agg", "nonexistent")
	if !errors.Is(err, flowstep.ErrInstanceNotFound) {
		t.Errorf("expected ErrInstanceNotFound, got %v", err)
	}
}
