package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// TaskStore persists pending tasks for human-in-the-loop workflows.
type TaskStore interface {
	Create(ctx context.Context, tx any, task types.PendingTask) error
	Get(ctx context.Context, taskID string) (*types.PendingTask, error)
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.PendingTask, error)
	Complete(ctx context.Context, tx any, taskID, choice, actorID string) error
	ListPending(ctx context.Context) ([]types.PendingTask, error)
	ListExpired(ctx context.Context) ([]types.PendingTask, error)
}
