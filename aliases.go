package flowstep

import (
	"context"
	"time"

	"github.com/mawkeye/flowstep/types"
)

// Store interfaces — re-exported from types/ for the public API surface.
type (
	// EventStore persists and queries immutable domain events.
	EventStore = types.EventStore

	// InstanceStore persists and queries workflow instances.
	// Update MUST use optimistic locking (WHERE updated_at = $old).
	// Returns ErrConcurrentModification if the row was changed since last read.
	InstanceStore = types.InstanceStore

	// TaskStore persists pending tasks for human-in-the-loop workflows.
	TaskStore = types.TaskStore

	// ChildStore tracks parent-child workflow relationships.
	ChildStore = types.ChildStore

	// ActivityStore tracks dispatched activity invocations.
	ActivityStore = types.ActivityStore
)

// Infrastructure interfaces — re-exported from types/ for the public API surface.
type (
	// TxProvider manages database transactions.
	TxProvider = types.TxProvider

	// EventBus publishes domain events to external subscribers.
	EventBus = types.EventBus

	// ActivityRunner dispatches activity invocations for async execution.
	ActivityRunner = types.ActivityRunner

	// Activity performs non-deterministic work outside the workflow transaction.
	// Can contain any code: API calls, DB writes, file I/O, network requests.
	// flowstep does NOT recover or replay activity state on failure.
	Activity = types.Activity

	// Clock provides deterministic time for the engine.
	Clock = types.Clock

	// Hooks allows consumers to observe engine behavior.
	// All methods MUST be non-blocking.
	Hooks = types.Hooks
)

// RealClock uses time.Now().
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }

// NoopHooks is the default Hooks implementation. Does nothing.
type NoopHooks struct{}

func (NoopHooks) OnTransition(context.Context, types.TransitionResult, time.Duration)                {}
func (NoopHooks) OnGuardFailed(context.Context, string, string, string, error)                       {}
func (NoopHooks) OnActivityDispatched(context.Context, types.ActivityInvocation)                      {}
func (NoopHooks) OnActivityCompleted(context.Context, types.ActivityInvocation, *types.ActivityResult) {}
func (NoopHooks) OnActivityFailed(context.Context, types.ActivityInvocation, error)                   {}
func (NoopHooks) OnStuck(context.Context, types.WorkflowInstance, string)                             {}
func (NoopHooks) OnPostCommitError(context.Context, string, error)                                    {}
