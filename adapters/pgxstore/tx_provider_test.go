package pgxstore_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate/adapters/pgxstore"
)

func TestTxProvider(t *testing.T) {
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

	// Ensure table exists (using instances table for test)
	if _, err := pool.Exec(ctx, "TRUNCATE flowstate_instances"); err != nil {
		t.Fatalf("failed to truncate: %v", err)
	}

	txProvider := pgxstore.NewTxProvider(pool)

	// Test Commit
	t.Run("BeginCommit", func(t *testing.T) {
		tx, err := txProvider.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin failed: %v", err)
		}
		if err := txProvider.Commit(ctx, tx); err != nil {
			t.Fatalf("Commit failed: %v", err)
		}
	})
}
