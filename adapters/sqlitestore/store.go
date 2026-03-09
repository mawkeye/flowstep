// Package sqlitestore provides a SQLite implementation of flowstate store interfaces
// using database/sql. Users must import a SQLite driver (e.g., modernc.org/sqlite
// or github.com/mattn/go-sqlite3).
package sqlitestore

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// Migrations contains the SQL migration files for flowstate.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// TxProvider implements flowstate.TxProvider using database/sql transactions.
type TxProvider struct {
	db *sql.DB
}

// NewTxProvider creates a new TxProvider.
func NewTxProvider(db *sql.DB) *TxProvider {
	return &TxProvider{db: db}
}

func (p *TxProvider) Begin(ctx context.Context) (any, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: begin tx: %w", err)
	}
	return tx, nil
}

func (p *TxProvider) Commit(_ context.Context, tx any) error {
	return tx.(*sql.Tx).Commit()
}

func (p *TxProvider) Rollback(_ context.Context, tx any) error {
	return tx.(*sql.Tx).Rollback()
}

// EventStore implements flowstate.EventStore using SQLite.
type EventStore struct {
	db *sql.DB
}

// NewEventStore creates a new SQLite EventStore.
func NewEventStore(db *sql.DB) *EventStore {
	return &EventStore{db: db}
}

func (s *EventStore) Append(ctx context.Context, tx any, event types.DomainEvent) error {
	sqlTx := tx.(*sql.Tx)
	stateBefore, _ := json.Marshal(event.StateBefore)
	stateAfter, _ := json.Marshal(event.StateAfter)
	payload, _ := json.Marshal(event.Payload)

	_, err := sqlTx.ExecContext(ctx, `
		INSERT INTO flowstate_events
			(id, aggregate_type, aggregate_id, workflow_type, workflow_version,
			 event_type, correlation_id, causation_id, actor_id, transition_name,
			 state_before, state_after, payload, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		event.ID, event.AggregateType, event.AggregateID,
		event.WorkflowType, event.WorkflowVersion,
		event.EventType, event.CorrelationID, event.CausationID,
		event.ActorID, event.TransitionName,
		string(stateBefore), string(stateAfter), string(payload),
		event.CreatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlitestore: insert event: %w", err)
	}
	return nil
}

func (s *EventStore) ListByCorrelation(ctx context.Context, correlationID string) ([]types.DomainEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, aggregate_type, aggregate_id, workflow_type, workflow_version,
		       event_type, correlation_id, causation_id, actor_id, transition_name,
		       state_before, state_after, payload, created_at
		FROM flowstate_events WHERE correlation_id = ? ORDER BY created_at`, correlationID)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func (s *EventStore) ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, aggregate_type, aggregate_id, workflow_type, workflow_version,
		       event_type, correlation_id, causation_id, actor_id, transition_name,
		       state_before, state_after, payload, created_at
		FROM flowstate_events WHERE aggregate_type = ? AND aggregate_id = ? ORDER BY created_at`,
		aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: query events: %w", err)
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]types.DomainEvent, error) {
	var events []types.DomainEvent
	for rows.Next() {
		var e types.DomainEvent
		var stateBefore, stateAfter, payload, createdAt string
		err := rows.Scan(
			&e.ID, &e.AggregateType, &e.AggregateID,
			&e.WorkflowType, &e.WorkflowVersion,
			&e.EventType, &e.CorrelationID, &e.CausationID,
			&e.ActorID, &e.TransitionName,
			&stateBefore, &stateAfter, &payload, &createdAt,
		)
		if err != nil {
			return nil, fmt.Errorf("sqlitestore: scan event: %w", err)
		}
		_ = json.Unmarshal([]byte(stateBefore), &e.StateBefore)
		_ = json.Unmarshal([]byte(stateAfter), &e.StateAfter)
		_ = json.Unmarshal([]byte(payload), &e.Payload)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		events = append(events, e)
	}
	return events, rows.Err()
}

// InstanceStore implements flowstate.InstanceStore using SQLite.
type InstanceStore struct {
	db          *sql.DB
	errNotFound error
}

// NewInstanceStore creates a new SQLite InstanceStore.
func NewInstanceStore(db *sql.DB, errNotFound error) *InstanceStore {
	return &InstanceStore{db: db, errNotFound: errNotFound}
}

func (s *InstanceStore) Get(ctx context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error) {
	var inst types.WorkflowInstance
	var stateData, createdAt, updatedAt string
	var isStuck int

	err := s.db.QueryRowContext(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at
		FROM flowstate_instances WHERE aggregate_type = ? AND aggregate_id = ?`,
		aggregateType, aggregateID,
	).Scan(
		&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
		&inst.AggregateType, &inst.AggregateID,
		&inst.CurrentState, &stateData, &inst.CorrelationID,
		&isStuck, &inst.StuckReason, &inst.RetryCount,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, s.errNotFound
		}
		return nil, fmt.Errorf("sqlitestore: get instance: %w", err)
	}
	inst.IsStuck = isStuck != 0
	_ = json.Unmarshal([]byte(stateData), &inst.StateData)
	inst.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	inst.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &inst, nil
}

func (s *InstanceStore) Create(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	sqlTx := tx.(*sql.Tx)
	stateData, _ := json.Marshal(instance.StateData)
	isStuck := 0
	if instance.IsStuck {
		isStuck = 1
	}

	_, err := sqlTx.ExecContext(ctx, `
		INSERT INTO flowstate_instances
			(id, workflow_type, workflow_version, aggregate_type, aggregate_id,
			 current_state, state_data, correlation_id, is_stuck, stuck_reason,
			 retry_count, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		instance.ID, instance.WorkflowType, instance.WorkflowVersion,
		instance.AggregateType, instance.AggregateID,
		instance.CurrentState, string(stateData), instance.CorrelationID,
		isStuck, instance.StuckReason, instance.RetryCount,
		instance.CreatedAt.Format(time.RFC3339Nano),
		instance.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlitestore: create instance: %w", err)
	}
	return nil
}

func (s *InstanceStore) Update(ctx context.Context, tx any, instance types.WorkflowInstance) error {
	sqlTx := tx.(*sql.Tx)
	stateData, _ := json.Marshal(instance.StateData)
	isStuck := 0
	if instance.IsStuck {
		isStuck = 1
	}

	result, err := sqlTx.ExecContext(ctx, `
		UPDATE flowstate_instances
		SET current_state = ?, state_data = ?, is_stuck = ?, stuck_reason = ?,
		    retry_count = ?, updated_at = ?
		WHERE aggregate_type = ? AND aggregate_id = ? AND updated_at = ?`,
		instance.CurrentState, string(stateData), isStuck, instance.StuckReason,
		instance.RetryCount, instance.UpdatedAt.Format(time.RFC3339Nano),
		instance.AggregateType, instance.AggregateID,
		instance.LastReadUpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return fmt.Errorf("sqlitestore: update instance: %w", err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return fmt.Errorf("sqlitestore: update instance: %w", flowstate.ErrConcurrentModification)
	}
	return nil
}

func (s *InstanceStore) ListStuck(ctx context.Context) ([]types.WorkflowInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workflow_type, workflow_version, aggregate_type, aggregate_id,
		       current_state, state_data, correlation_id, is_stuck, stuck_reason,
		       retry_count, created_at, updated_at
		FROM flowstate_instances WHERE is_stuck = 1 ORDER BY updated_at`)
	if err != nil {
		return nil, fmt.Errorf("sqlitestore: query stuck: %w", err)
	}
	defer rows.Close()

	var instances []types.WorkflowInstance
	for rows.Next() {
		var inst types.WorkflowInstance
		var stateData, createdAt, updatedAt string
		var isStuck int
		err := rows.Scan(
			&inst.ID, &inst.WorkflowType, &inst.WorkflowVersion,
			&inst.AggregateType, &inst.AggregateID,
			&inst.CurrentState, &stateData, &inst.CorrelationID,
			&isStuck, &inst.StuckReason, &inst.RetryCount,
			&createdAt, &updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("sqlitestore: scan instance: %w", err)
		}
		inst.IsStuck = isStuck != 0
		_ = json.Unmarshal([]byte(stateData), &inst.StateData)
		inst.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		inst.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		instances = append(instances, inst)
	}
	return instances, rows.Err()
}
