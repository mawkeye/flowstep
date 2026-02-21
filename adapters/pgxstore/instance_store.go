package pgxstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// InstanceStore implements flowstate.InstanceStore using PostgreSQL.
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
	var stateData []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at
		FROM flowstate_instances
		WHERE aggregate_type = $1 AND aggregate_id = $2`,
		aggregateType, aggregateID,
	).Scan(
		&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
		&inst.AggregateType, &inst.AggregateID,
		&inst.CurrentState, &stateData, &inst.CorrelationID,
		&inst.IsStuck, &inst.StuckReason, &inst.RetryCount,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, s.errNotFound
		}
		return nil, fmt.Errorf("pgxstore: get instance: %w", err)
	}
	_ = json.Unmarshal(stateData, &inst.StateData)
	return &inst, nil
}

func (s *InstanceStore) Create(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	pgxTx, err := getTx(tx)
	if err != nil {
		return err
	}

	stateData, _ := json.Marshal(instance.StateData)
	_, err = pgxTx.Exec(ctx, `
		INSERT INTO flowstate_instances
			(id, workflow_type, workflow_version, aggregate_type, aggregate_id,
			 current_state, state_data, correlation_id, is_stuck, stuck_reason,
			 retry_count, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		instance.ID, instance.WorkflowType, instance.WorkflowVersion,
		instance.AggregateType, instance.AggregateID,
		instance.CurrentState, stateData, instance.CorrelationID,
		instance.IsStuck, instance.StuckReason, instance.RetryCount,
		instance.CreatedAt, instance.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("pgxstore: create instance: %w", err)
	}
	return nil
}

func (s *InstanceStore) Update(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	pgxTx, err := getTx(tx)
	if err != nil {
		return err
	}

	stateData, _ := json.Marshal(instance.StateData)
	tag, err := pgxTx.Exec(ctx, `
		UPDATE flowstate_instances
		SET current_state = $1, state_data = $2, is_stuck = $3, stuck_reason = $4,
		    retry_count = $5, updated_at = $6
		WHERE aggregate_type = $7 AND aggregate_id = $8`,
		instance.CurrentState, stateData, instance.IsStuck, instance.StuckReason,
		instance.RetryCount, instance.UpdatedAt,
		instance.AggregateType, instance.AggregateID,
	)
	if err != nil {
		return fmt.Errorf("pgxstore: update instance: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pgxstore: update instance: %w", flowstate.ErrConcurrentModification)
	}
	return nil
}

func (s *InstanceStore) ListStuck(ctx context.Context) ([]types.WorkflowInstance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at
		FROM flowstate_instances
		WHERE is_stuck = TRUE
		ORDER BY updated_at`)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query stuck instances: %w", err)
	}
	defer rows.Close()

	var instances []types.WorkflowInstance
	for rows.Next() {
		var inst types.WorkflowInstance
		var stateData []byte
		err := rows.Scan(
			&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
			&inst.AggregateType, &inst.AggregateID,
			&inst.CurrentState, &stateData, &inst.CorrelationID,
			&inst.IsStuck, &inst.StuckReason, &inst.RetryCount,
			&inst.CreatedAt, &inst.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan instance: %w", err)
		}
		_ = json.Unmarshal(stateData, &inst.StateData)
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}
