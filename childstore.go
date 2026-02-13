package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// ChildStore tracks parent-child workflow relationships.
type ChildStore interface {
	Create(ctx context.Context, tx any, relation types.ChildRelation) error
	GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*types.ChildRelation, error)
	GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]types.ChildRelation, error)
	GetByGroup(ctx context.Context, groupID string) ([]types.ChildRelation, error)
	Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error
}
