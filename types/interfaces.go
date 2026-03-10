package types

import (
	"context"
	"time"
)

// EventStore persists and queries immutable domain events.
type EventStore interface {
	Append(ctx context.Context, tx any, event DomainEvent) error
	ListByCorrelation(ctx context.Context, correlationID string) ([]DomainEvent, error)
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]DomainEvent, error)
}

// InstanceStore persists and queries workflow instances.
// Update MUST use optimistic locking (compare instance.LastReadUpdatedAt against stored UpdatedAt).
// Returns ErrConcurrentModification if the row was changed since last read.
type InstanceStore interface {
	Get(ctx context.Context, aggregateType, aggregateID string) (*WorkflowInstance, error)
	Create(ctx context.Context, tx any, instance WorkflowInstance) error
	Update(ctx context.Context, tx any, instance WorkflowInstance) error
	ListStuck(ctx context.Context) ([]WorkflowInstance, error)
}

// TxProvider manages database transactions.
type TxProvider interface {
	Begin(ctx context.Context) (tx any, err error)
	Commit(ctx context.Context, tx any) error
	Rollback(ctx context.Context, tx any) error
}

// EventBus publishes domain events to external subscribers.
type EventBus interface {
	Emit(ctx context.Context, event DomainEvent) error
}

// ChildStore tracks parent-child workflow relationships.
type ChildStore interface {
	Create(ctx context.Context, tx any, relation ChildRelation) error
	GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*ChildRelation, error)
	GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]ChildRelation, error)
	GetByGroup(ctx context.Context, groupID string) ([]ChildRelation, error)
	Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error
}

// TaskStore persists pending tasks for human-in-the-loop workflows.
type TaskStore interface {
	Create(ctx context.Context, tx any, task PendingTask) error
	Get(ctx context.Context, taskID string) (*PendingTask, error)
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]PendingTask, error)
	Complete(ctx context.Context, tx any, taskID, choice, actorID string) error
	ListPending(ctx context.Context) ([]PendingTask, error)
	ListExpired(ctx context.Context) ([]PendingTask, error)
}

// ActivityStore tracks dispatched activity invocations.
type ActivityStore interface {
	Create(ctx context.Context, tx any, invocation ActivityInvocation) error
	Get(ctx context.Context, invocationID string) (*ActivityInvocation, error)
	UpdateStatus(ctx context.Context, invocationID, status string, result *ActivityResult) error
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]ActivityInvocation, error)
	ListPending(ctx context.Context) ([]ActivityInvocation, error)
	ListFailed(ctx context.Context) ([]ActivityInvocation, error)
	ListRetryable(ctx context.Context) ([]ActivityInvocation, error)
}

// ActivityRunner dispatches activity invocations for async execution.
type ActivityRunner interface {
	Dispatch(ctx context.Context, invocation ActivityInvocation) error
}

// Activity performs non-deterministic work outside the workflow transaction.
// Can contain any code: API calls, DB writes, file I/O, network requests.
// flowstep does NOT recover or replay activity state on failure.
type Activity interface {
	Name() string
	Execute(ctx context.Context, input ActivityInput) (*ActivityResult, error)
}

// Clock provides deterministic time for the engine.
type Clock interface {
	Now() time.Time
}

