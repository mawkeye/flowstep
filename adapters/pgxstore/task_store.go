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

// TaskStore implements flowstate.TaskStore using PostgreSQL.
type TaskStore struct {
	pool *pgxpool.Pool
}

// NewTaskStore creates a new PostgreSQL TaskStore.
func NewTaskStore(pool *pgxpool.Pool) *TaskStore {
	return &TaskStore{pool: pool}
}

func (s *TaskStore) Create(ctx context.Context, tx any, task types.PendingTask) error {
	pgxTx, err := getTx(tx)
	if err != nil {
		// Allow nil tx for post-commit operations
		_, err = s.pool.Exec(ctx, s.insertSQL(),
			task.ID, task.WorkflowType, task.AggregateType, task.AggregateID,
			task.CorrelationID, task.TaskType, task.Description,
			mustJSON(task.Options), task.Status, task.Timeout.Nanoseconds(),
			nilTime(task.ExpiresAt), task.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("pgxstore: create task: %w", err)
		}
		return nil
	}

	_, err = pgxTx.Exec(ctx, s.insertSQL(),
		task.ID, task.WorkflowType, task.AggregateType, task.AggregateID,
		task.CorrelationID, task.TaskType, task.Description,
		mustJSON(task.Options), task.Status, task.Timeout.Nanoseconds(),
		nilTime(task.ExpiresAt), task.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("pgxstore: create task: %w", err)
	}
	return nil
}

func (s *TaskStore) insertSQL() string {
	return `INSERT INTO flowstate_tasks
		(id, workflow_type, aggregate_type, aggregate_id, correlation_id,
		 task_type, description, options, status, timeout, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
}

func (s *TaskStore) Get(ctx context.Context, taskID string) (*types.PendingTask, error) {
	var task types.PendingTask
	var options []byte
	var timeout int64
	var expiresAt *time.Time

	err := s.pool.QueryRow(ctx, `
		SELECT id, workflow_type, aggregate_type, aggregate_id, correlation_id,
		       task_type, description, options, status, choice, completed_by,
		       timeout, expires_at, created_at
		FROM flowstate_tasks WHERE id = $1`, taskID,
	).Scan(
		&task.ID, &task.WorkflowType, &task.AggregateType, &task.AggregateID,
		&task.CorrelationID, &task.TaskType, &task.Description,
		&options, &task.Status, &task.Choice, &task.CompletedBy,
		&timeout, &expiresAt, &task.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("pgxstore: task not found")
		}
		return nil, fmt.Errorf("pgxstore: get task: %w", err)
	}
	_ = json.Unmarshal(options, &task.Options)
	task.Timeout = time.Duration(timeout)
	if expiresAt != nil {
		task.ExpiresAt = *expiresAt
	}
	return &task, nil
}

func (s *TaskStore) GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.PendingTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_type, aggregate_type, aggregate_id, correlation_id,
		       task_type, description, options, status, choice, completed_by,
		       timeout, expires_at, created_at
		FROM flowstate_tasks
		WHERE aggregate_type = $1 AND aggregate_id = $2
		ORDER BY created_at`, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

func (s *TaskStore) ListPending(ctx context.Context) ([]types.PendingTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_type, aggregate_type, aggregate_id, correlation_id,
		       task_type, description, options, status, choice, completed_by,
		       timeout, expires_at, created_at
		FROM flowstate_tasks
		WHERE status = 'PENDING'
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: list pending tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

func (s *TaskStore) ListExpired(ctx context.Context) ([]types.PendingTask, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_type, aggregate_type, aggregate_id, correlation_id,
		       task_type, description, options, status, choice, completed_by,
		       timeout, expires_at, created_at
		FROM flowstate_tasks
		WHERE status = 'PENDING' AND expires_at < NOW()
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: list expired tasks: %w", err)
	}
	defer rows.Close()

	return s.scanTasks(rows)
}

func (s *TaskStore) scanTasks(rows pgx.Rows) ([]types.PendingTask, error) {
	var tasks []types.PendingTask
	for rows.Next() {
		var task types.PendingTask
		var options []byte
		var timeout int64
		var expiresAt *time.Time
		err := rows.Scan(
			&task.ID, &task.WorkflowType, &task.AggregateType, &task.AggregateID,
			&task.CorrelationID, &task.TaskType, &task.Description,
			&options, &task.Status, &task.Choice, &task.CompletedBy,
			&timeout, &expiresAt, &task.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan task: %w", err)
		}
		_ = json.Unmarshal(options, &task.Options)
		task.Timeout = time.Duration(timeout)
		if expiresAt != nil {
			task.ExpiresAt = *expiresAt
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *TaskStore) Complete(ctx context.Context, tx any, taskID, choice, actorID string) error {
	query := `UPDATE flowstate_tasks SET status = 'COMPLETED', choice = $1, completed_by = $2, completed_at = NOW() WHERE id = $3`

	if tx != nil {
		pgxTx, err := getTx(tx)
		if err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, query, choice, actorID, taskID)
		if err != nil {
			return fmt.Errorf("pgxstore: complete task: %w", err)
		}
		return nil
	}

	_, err := s.pool.Exec(ctx, query, choice, actorID, taskID)
	if err != nil {
		return fmt.Errorf("pgxstore: complete task: %w", err)
	}
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func nilTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
