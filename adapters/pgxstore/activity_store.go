package pgxstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate/types"
)

// ActivityStore implements flowstate.ActivityStore using PostgreSQL.
type ActivityStore struct {
	pool *pgxpool.Pool
}

// NewActivityStore creates a new PostgreSQL ActivityStore.
func NewActivityStore(pool *pgxpool.Pool) *ActivityStore {
	return &ActivityStore{pool: pool}
}

func (s *ActivityStore) Create(ctx context.Context, tx any, inv types.ActivityInvocation) error {
	input, _ := json.Marshal(inv.Input)
	retryPolicy, _ := json.Marshal(inv.RetryPolicy)

	query := `INSERT INTO flowstate_activities
		(id, activity_name, workflow_type, aggregate_type, aggregate_id,
		 correlation_id, mode, input, retry_policy, timeout, status,
		 max_attempts, scheduled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`

	args := []any{
		inv.ID, inv.ActivityName, inv.WorkflowType,
		inv.AggregateType, inv.AggregateID, inv.CorrelationID,
		string(inv.Mode), input, retryPolicy,
		inv.Timeout.Nanoseconds(), inv.Status,
		inv.MaxAttempts, inv.ScheduledAt,
	}

	if tx != nil {
		pgxTx, err := getTx(tx)
		if err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("pgxstore: create activity: %w", err)
		}
		return nil
	}

	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("pgxstore: create activity: %w", err)
	}
	return nil
}

func (s *ActivityStore) Get(ctx context.Context, invocationID string) (*types.ActivityInvocation, error) {
	var inv types.ActivityInvocation
	var mode string
	var input, output, retryPolicy []byte
	var timeout int64
	var errorMsg string
	var attempts int

	err := s.pool.QueryRow(ctx, `
		SELECT id, activity_name, workflow_type, aggregate_type, aggregate_id,
		       correlation_id, mode, input, output, error_msg, retry_policy,
		       timeout, status, max_attempts, attempt_count,
		       scheduled_at, started_at, completed_at
		FROM flowstate_activities WHERE id = $1`, invocationID,
	).Scan(
		&inv.ID, &inv.ActivityName, &inv.WorkflowType,
		&inv.AggregateType, &inv.AggregateID, &inv.CorrelationID,
		&mode, &input, &output, &errorMsg, &retryPolicy,
		&timeout, &inv.Status, &inv.MaxAttempts, &attempts,
		&inv.ScheduledAt, &inv.StartedAt, &inv.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("pgxstore: activity not found")
		}
		return nil, fmt.Errorf("pgxstore: get activity: %w", err)
	}

	inv.Mode = types.DispatchMode(mode)
	inv.Attempts = attempts
	_ = json.Unmarshal(input, &inv.Input)
	if output != nil {
		var result types.ActivityResult
		_ = json.Unmarshal(output, &result)
		inv.Result = &result
	}
	_ = json.Unmarshal(retryPolicy, &inv.RetryPolicy)
	inv.Timeout = time.Duration(timeout)
	return &inv, nil
}

func (s *ActivityStore) UpdateStatus(ctx context.Context, invocationID, status string, result *types.ActivityResult) error {
	var output []byte
	var errorMsg string
	if result != nil {
		output, _ = json.Marshal(result)
		errorMsg = result.Error
	}

	_, err := s.pool.Exec(ctx, `
		UPDATE flowstate_activities
		SET status = $1, output = $2, error_msg = $3, completed_at = NOW()
		WHERE id = $4`, status, output, errorMsg, invocationID)
	if err != nil {
		return fmt.Errorf("pgxstore: update activity status: %w", err)
	}
	return nil
}

func (s *ActivityStore) ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.ActivityInvocation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, activity_name, workflow_type, aggregate_type, aggregate_id,
		       correlation_id, mode, input, output, error_msg, retry_policy,
		       timeout, status, max_attempts, attempt_count,
		       scheduled_at, started_at, completed_at
		FROM flowstate_activities
		WHERE aggregate_type = $1 AND aggregate_id = $2
		ORDER BY scheduled_at`, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query activities: %w", err)
	}
	defer rows.Close()

	var invocations []types.ActivityInvocation
	for rows.Next() {
		var inv types.ActivityInvocation
		var mode string
		var input, output, retryPolicy []byte
		var timeout int64
		var errorMsg string
		var attempts int

		err := rows.Scan(
			&inv.ID, &inv.ActivityName, &inv.WorkflowType,
			&inv.AggregateType, &inv.AggregateID, &inv.CorrelationID,
			&mode, &input, &output, &errorMsg, &retryPolicy,
			&timeout, &inv.Status, &inv.MaxAttempts, &attempts,
			&inv.ScheduledAt, &inv.StartedAt, &inv.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan activity: %w", err)
		}
		inv.Mode = types.DispatchMode(mode)
		inv.Attempts = attempts
		_ = json.Unmarshal(input, &inv.Input)
		if output != nil {
			var result types.ActivityResult
			_ = json.Unmarshal(output, &result)
			inv.Result = &result
		}
		_ = json.Unmarshal(retryPolicy, &inv.RetryPolicy)
		inv.Timeout = time.Duration(timeout)
		invocations = append(invocations, inv)
	}
	return invocations, rows.Err()
}
