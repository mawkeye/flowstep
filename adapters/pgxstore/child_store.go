package pgxstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mawkeye/flowstep/types"
)

// ChildStore implements flowstep.ChildStore using PostgreSQL.
type ChildStore struct {
	pool *pgxpool.Pool
}

// NewChildStore creates a new PostgreSQL ChildStore.
func NewChildStore(pool *pgxpool.Pool) *ChildStore {
	return &ChildStore{pool: pool}
}

func (s *ChildStore) Create(ctx context.Context, tx any, relation types.ChildRelation) error {
	query := `INSERT INTO flowstep_children
		(id, group_id, parent_workflow_type, parent_aggregate_type, parent_aggregate_id,
		 child_workflow_type, child_aggregate_type, child_aggregate_id,
		 correlation_id, status, join_policy, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

	args := []any{
		relation.ID, nilString(relation.GroupID),
		relation.ParentWorkflowType, relation.ParentAggregateType, relation.ParentAggregateID,
		relation.ChildWorkflowType, relation.ChildAggregateType, relation.ChildAggregateID,
		relation.CorrelationID, relation.Status, nilString(relation.JoinPolicy),
		relation.CreatedAt,
	}

	if tx != nil {
		pgxTx, err := getTx(tx)
		if err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("pgxstore: create child relation: %w", err)
		}
		return nil
	}

	_, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("pgxstore: create child relation: %w", err)
	}
	return nil
}

func (s *ChildStore) GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*types.ChildRelation, error) {
	var r types.ChildRelation
	var groupID, joinPolicy *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, group_id, parent_workflow_type, parent_aggregate_type, parent_aggregate_id,
		       child_workflow_type, child_aggregate_type, child_aggregate_id,
		       correlation_id, status, child_terminal_state, join_policy, created_at, completed_at
		FROM flowstep_children
		WHERE child_aggregate_type = $1 AND child_aggregate_id = $2`,
		childAggregateType, childAggregateID,
	).Scan(
		&r.ID, &groupID, &r.ParentWorkflowType, &r.ParentAggregateType, &r.ParentAggregateID,
		&r.ChildWorkflowType, &r.ChildAggregateType, &r.ChildAggregateID,
		&r.CorrelationID, &r.Status, &r.ChildTerminalState, &joinPolicy,
		&r.CreatedAt, &r.CompletedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: get child relation: %w", err)
	}
	if groupID != nil {
		r.GroupID = *groupID
	}
	if joinPolicy != nil {
		r.JoinPolicy = *joinPolicy
	}
	return &r, nil
}

func (s *ChildStore) GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]types.ChildRelation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, group_id, parent_workflow_type, parent_aggregate_type, parent_aggregate_id,
		       child_workflow_type, child_aggregate_type, child_aggregate_id,
		       correlation_id, status, child_terminal_state, join_policy, created_at, completed_at
		FROM flowstep_children
		WHERE parent_aggregate_type = $1 AND parent_aggregate_id = $2`,
		parentAggregateType, parentAggregateID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query children: %w", err)
	}
	defer rows.Close()

	return scanRelations(rows)
}

func (s *ChildStore) GetByGroup(ctx context.Context, groupID string) ([]types.ChildRelation, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, group_id, parent_workflow_type, parent_aggregate_type, parent_aggregate_id,
		       child_workflow_type, child_aggregate_type, child_aggregate_id,
		       correlation_id, status, child_terminal_state, join_policy, created_at, completed_at
		FROM flowstep_children
		WHERE group_id = $1`, groupID)
	if err != nil {
		return nil, fmt.Errorf("pgxstore: query children by group: %w", err)
	}
	defer rows.Close()

	return scanRelations(rows)
}

func (s *ChildStore) Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error {
	query := `UPDATE flowstep_children SET status = 'COMPLETED', child_terminal_state = $1, completed_at = NOW()
		WHERE child_aggregate_type = $2 AND child_aggregate_id = $3`

	if tx != nil {
		pgxTx, err := getTx(tx)
		if err != nil {
			return err
		}
		_, err = pgxTx.Exec(ctx, query, terminalState, childAggregateType, childAggregateID)
		if err != nil {
			return fmt.Errorf("pgxstore: complete child: %w", err)
		}
		return nil
	}

	_, err := s.pool.Exec(ctx, query, terminalState, childAggregateType, childAggregateID)
	if err != nil {
		return fmt.Errorf("pgxstore: complete child: %w", err)
	}
	return nil
}

type scannable interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanRelations(rows scannable) ([]types.ChildRelation, error) {
	var relations []types.ChildRelation
	for rows.Next() {
		var r types.ChildRelation
		var groupID, joinPolicy *string
		err := rows.Scan(
			&r.ID, &groupID, &r.ParentWorkflowType, &r.ParentAggregateType, &r.ParentAggregateID,
			&r.ChildWorkflowType, &r.ChildAggregateType, &r.ChildAggregateID,
			&r.CorrelationID, &r.Status, &r.ChildTerminalState, &joinPolicy,
			&r.CreatedAt, &r.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("pgxstore: scan relation: %w", err)
		}
		if groupID != nil {
			r.GroupID = *groupID
		}
		if joinPolicy != nil {
			r.JoinPolicy = *joinPolicy
		}
		relations = append(relations, r)
	}
	return relations, rows.Err()
}

func nilString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
