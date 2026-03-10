package types

import (
	"context"
	"time"
)

// Observer is a marker interface for all observer types.
// Pass concrete observer values to WithObservers() on the engine.
type Observer any

// TransitionObserver receives notifications after each successful transition.
// Implementations MUST be non-blocking.
type TransitionObserver interface {
	Observer
	OnTransition(ctx context.Context, event TransitionEvent)
}

// GuardObserver receives notifications when a guard rejects a transition.
// Implementations MUST be non-blocking.
type GuardObserver interface {
	Observer
	OnGuardFailed(ctx context.Context, event GuardFailureEvent)
}

// ActivityObserver receives notifications for activity lifecycle events.
// Implementations MUST be non-blocking.
type ActivityObserver interface {
	Observer
	OnActivityDispatched(ctx context.Context, event ActivityDispatchedEvent)
	OnActivityCompleted(ctx context.Context, event ActivityCompletedEvent)
	OnActivityFailed(ctx context.Context, event ActivityFailedEvent)
}

// InfrastructureObserver receives notifications for infrastructure-level events.
// Implementations MUST be non-blocking.
type InfrastructureObserver interface {
	Observer
	OnStuck(ctx context.Context, event StuckEvent)
	OnPostCommitError(ctx context.Context, event PostCommitErrorEvent)
}

// ─── Event structs ────────────────────────────────────────────────────────────

// TransitionEvent is delivered to TransitionObserver after a successful transition.
type TransitionEvent struct {
	Result   TransitionResult
	Duration time.Duration
}

// GuardFailureEvent is delivered to GuardObserver when a guard rejects a transition.
type GuardFailureEvent struct {
	WorkflowType   string
	TransitionName string
	GuardName      string
	Err            error
}

// ActivityDispatchedEvent is delivered to ActivityObserver when an activity is dispatched.
type ActivityDispatchedEvent struct {
	Invocation ActivityInvocation
}

// ActivityCompletedEvent is delivered to ActivityObserver when an activity completes successfully.
type ActivityCompletedEvent struct {
	Invocation ActivityInvocation
	Result     *ActivityResult
}

// ActivityFailedEvent is delivered to ActivityObserver when an activity fails.
type ActivityFailedEvent struct {
	Invocation ActivityInvocation
	Err        error
}

// StuckEvent is delivered to InfrastructureObserver when a workflow instance becomes stuck.
type StuckEvent struct {
	Instance WorkflowInstance
	Reason   string
}

// PostCommitErrorEvent is delivered to InfrastructureObserver when a post-commit operation fails.
// The transition is still considered successful — the state change has been committed.
type PostCommitErrorEvent struct {
	Operation string
	Err       error
}
