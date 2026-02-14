package pgxstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstate/types"
)

// EventStore implements flowstate.EventStore using PostgreSQL.
type EventStore struct {
	pool *pgxpool.Pool
}

// NewEventStore creates a new PostgreSQL EventStore.
func NewEventStore(pool *pgxpool.Pool) *EventStore {
	return &EventStore{pool: pool}
}

func (s *EventStore) Append(ctx context.Context, tx any, event types.DomainEvent) error {
	pgxTx, err := getTx(tx)
	if err != nil {
		return err
	}

	stateBefore, _ := json.Marshal(event.StateBefore)
	stateAfter, _ := json.Marshal(event.StateAfter)
	payload, _ := json.Marshal(event.Payload)

	_, err = pgxTx.Exec(ctx, `
		INSERT INTO flowstate_events
			(id, aggregate_type, aggregate_id, workflow_type, workflow_version,
			 event_type, correlation_id, causation_id, actor_id, transition_name,
			 state_before, state_after, payload, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		event.ID, event.AggregateType, event.AggregateID,
		event.WorkflowType, event.WorkflowVersion,
		event.EventType, event.CorrelationID, event.CausationID,
		event.ActorID, event.TransitionName,
		stateBefore, stateAfter, payload, event.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("pgxstore: insert event: %w", err)
	}
	return nil
}

func (s *EventStore) ListByCorrelation(ctx context.Context, correlationID string) ([]types.DomainEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, workflow_type, workflow_version,
		       event_type, correlation_id, causation_id, actor_id, transition_name,
		       state_before, state_after, payload, created_at
		FROM flowstate_events
		WHERE correlation_id = $1
		ORDER BY created_at`, correlationID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

func (s *EventStore) ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, aggregate_type, aggregate_id, workflow_type, workflow_version,
		       event_type, correlation_id, causation_id, actor_id, transition_name,
		       state_before, state_after, payload, created_at
		FROM flowstate_events
		WHERE aggregate_type = $1 AND aggregate_id = $2
		ORDER BY created_at`, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query events: %w", err)
	}
	defer rows.Close()

	return scanEvents(rows)
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanEvents(rows rowScanner) ([]types.DomainEvent, error) {
	var events []types.DomainEvent
	for rows.Next() {
		var e types.DomainEvent
		var stateBefore, stateAfter, payload []byte
		err := rows.Scan(
			&e.ID, &e.AggregateType, &e.AggregateID,
			&e.WorkflowType, &e.WorkflowVersion,
			&e.EventType, &e.CorrelationID, &e.CausationID,
			&e.ActorID, &e.TransitionName,
			&stateBefore, &stateAfter, &payload, &e.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan event: %w", err)
		}
		_ = json.Unmarshal(stateBefore, &e.StateBefore)
		_ = json.Unmarshal(stateAfter, &e.StateAfter)
		_ = json.Unmarshal(payload, &e.Payload)
		events = append(events, e)
	}
	return events, rows.Err()
}
