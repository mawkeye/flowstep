package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// ActivityStore tracks dispatched activity invocations.
type ActivityStore interface {
	Create(ctx context.Context, tx any, invocation types.ActivityInvocation) error
	Get(ctx context.Context, invocationID string) (*types.ActivityInvocation, error)
	UpdateStatus(ctx context.Context, invocationID, status string, result *types.ActivityResult) error
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.ActivityInvocation, error)
	ListPending(ctx context.Context) ([]types.ActivityInvocation, error)
	ListFailed(ctx context.Context) ([]types.ActivityInvocation, error)
	ListRetryable(ctx context.Context) ([]types.ActivityInvocation, error)
}
