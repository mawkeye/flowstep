package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// EventBus publishes domain events to external subscribers.
type EventBus interface {
	Emit(ctx context.Context, event types.DomainEvent) error
}
