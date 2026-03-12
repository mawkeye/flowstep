package pgxstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// InstanceStore implements flowstep.InstanceStore using PostgreSQL.
type InstanceStore struct {
	pool        *pgxpool.Pool
	errNotFound error
}

// NewInstanceStore creates a new PostgreSQL InstanceStore.
// errNotFound is the sentinel error to return when an instance is not found.
func NewInstanceStore(pool *pgxpool.Pool, errNotFound error) *InstanceStore {
	return &InstanceStore{pool: pool, errNotFound: errNotFound}
}

func (s *InstanceStore) Get(ctx context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error) {
	var inst types.WorkflowInstance
	var stateData, shallowHistory, deepHistory, activeInParallel []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at,
		       shallow_history, deep_history, active_in_parallel, parallel_clock
		FROM flowstep_instances
		WHERE aggregate_type = $1 AND aggregate_id = $2`,
		aggregateType, aggregateID,
	).Scan(
		&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
		&inst.AggregateType, &inst.AggregateID,
		&inst.CurrentState, &stateData, &inst.CorrelationID,
		&inst.IsStuck, &inst.StuckReason, &inst.RetryCount,
		&inst.CreatedAt, &inst.UpdatedAt,
		&shallowHistory, &deepHistory, &activeInParallel, &inst.ParallelClock,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.errNotFound
		}
		return nil, fmt.Errorf("pgxstore: get instance: %w", err)
	}
	_ = json.Unmarshal(stateData, &inst.StateData)
	inst.ShallowHistory = make(map[string]string)
	inst.DeepHistory = make(map[string]string)
	inst.ActiveInParallel = make(map[string]string)
	_ = json.Unmarshal(shallowHistory, &inst.ShallowHistory)
	_ = json.Unmarshal(deepHistory, &inst.DeepHistory)
	_ = json.Unmarshal(activeInParallel, &inst.ActiveInParallel)
	return &inst, nil
}

func (s *InstanceStore) Create(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	const q = `
		INSERT INTO flowstep_instances
			(id, workflow_type, workflow_version, aggregate_type, aggregate_id,
			 current_state, state_data, correlation_id, is_stuck, stuck_reason,
			 retry_count, created_at, updated_at,
			 shallow_history, deep_history, active_in_parallel, parallel_clock)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`

	stateData, _ := json.Marshal(instance.StateData)
	shallowHistory, _ := json.Marshal(instance.ShallowHistory)
	deepHistory, _ := json.Marshal(instance.DeepHistory)
	activeInParallel, _ := json.Marshal(instance.ActiveInParallel)
	args := []any{
		instance.ID, instance.WorkflowType, instance.WorkflowVersion,
		instance.AggregateType, instance.AggregateID,
		instance.CurrentState, stateData, instance.CorrelationID,
		instance.IsStuck, instance.StuckReason, instance.RetryCount,
		instance.CreatedAt, instance.UpdatedAt,
		shallowHistory, deepHistory, activeInParallel, instance.ParallelClock,
	}

	pgxTx, err := getTx(tx)
	if err != nil {
		// nil tx: execute directly on the pool (e.g. in tests)
		if _, execErr := s.pool.Exec(ctx, q, args...); execErr != nil {
			return fmt.Errorf("pgxstore: create instance: %w", execErr)
		}
		return nil
	}
	if _, execErr := pgxTx.Exec(ctx, q, args...); execErr != nil {
		return fmt.Errorf("pgxstore: create instance: %w", execErr)
	}
	return nil
}

func (s *InstanceStore) Update(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	const q = `
		UPDATE flowstep_instances
		SET current_state = $1, state_data = $2, is_stuck = $3, stuck_reason = $4,
		    retry_count = $5, updated_at = $6,
		    shallow_history = $7, deep_history = $8,
		    active_in_parallel = $9, parallel_clock = $10
		WHERE aggregate_type = $11 AND aggregate_id = $12 AND updated_at = $13`

	stateData, _ := json.Marshal(instance.StateData)
	shallowHistory, _ := json.Marshal(instance.ShallowHistory)
	deepHistory, _ := json.Marshal(instance.DeepHistory)
	activeInParallel, _ := json.Marshal(instance.ActiveInParallel)
	args := []any{
		instance.CurrentState, stateData, instance.IsStuck, instance.StuckReason,
		instance.RetryCount, instance.UpdatedAt,
		shallowHistory, deepHistory,
		activeInParallel, instance.ParallelClock,
		instance.AggregateType, instance.AggregateID,
		instance.LastReadUpdatedAt,
	}

	var rowsAffected int64
	pgxTx, err := getTx(tx)
	if err != nil {
		// nil tx: execute directly on the pool (e.g. in tests)
		tag, execErr := s.pool.Exec(ctx, q, args...)
		if execErr != nil {
			return fmt.Errorf("pgxstore: update instance: %w", execErr)
		}
		rowsAffected = tag.RowsAffected()
	} else {
		tag, execErr := pgxTx.Exec(ctx, q, args...)
		if execErr != nil {
			return fmt.Errorf("pgxstore: update instance: %w", execErr)
		}
		rowsAffected = tag.RowsAffected()
	}

	if rowsAffected == 0 {
		return fmt.Errorf("pgxstore: update instance: %w", flowstep.ErrConcurrentModification)
	}
	return nil
}

func (s *InstanceStore) ListStuck(ctx context.Context) ([]types.WorkflowInstance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at,
		       shallow_history, deep_history, active_in_parallel, parallel_clock
		FROM flowstep_instances
		WHERE is_stuck = TRUE
		ORDER BY updated_at`)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query stuck instances: %w", err)
	}
	defer rows.Close()

	var instances []types.WorkflowInstance
	for rows.Next() {
		var inst types.WorkflowInstance
		var stateData, shallowHistory, deepHistory, activeInParallel []byte
		err := rows.Scan(
			&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
			&inst.AggregateType, &inst.AggregateID,
			&inst.CurrentState, &stateData, &inst.CorrelationID,
			&inst.IsStuck, &inst.StuckReason, &inst.RetryCount,
			&inst.CreatedAt, &inst.UpdatedAt,
			&shallowHistory, &deepHistory, &activeInParallel, &inst.ParallelClock,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan instance: %w", err)
		}
		_ = json.Unmarshal(stateData, &inst.StateData)
		inst.ShallowHistory = make(map[string]string)
		inst.DeepHistory = make(map[string]string)
		inst.ActiveInParallel = make(map[string]string)
		_ = json.Unmarshal(shallowHistory, &inst.ShallowHistory)
		_ = json.Unmarshal(deepHistory, &inst.DeepHistory)
		_ = json.Unmarshal(activeInParallel, &inst.ActiveInParallel)
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}
