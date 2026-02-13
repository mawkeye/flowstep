package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// EventStore persists and queries immutable domain events.
type EventStore interface {
	Append(ctx context.Context, tx any, event types.DomainEvent) error
	ListByCorrelation(ctx context.Context, correlationID string) ([]types.DomainEvent, error)
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error)
}

// InstanceStore persists and queries workflow instances.
// Update MUST use optimistic locking (WHERE updated_at = $old).
// Returns ErrConcurrentModification if the row was changed since last read.
type InstanceStore interface {
	Get(ctx context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error)
	Create(ctx context.Context, tx any, instance types.WorkflowInstance) error
	Update(ctx context.Context, tx any, instance types.WorkflowInstance) error
	ListStuck(ctx context.Context) ([]types.WorkflowInstance, error)
}
