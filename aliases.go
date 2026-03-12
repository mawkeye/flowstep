package flowstep

import (
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

	// ActivityResolver is an optional extension of ActivityRunner for synchronous name-based resolution.
	// Implement this on your ActivityRunner to enable entry/exit activity execution.
	ActivityResolver = types.ActivityResolver

	// Activity performs non-deterministic work outside the workflow transaction.
	// Can contain any code: API calls, DB writes, file I/O, network requests.
	// flowstep does NOT recover or replay activity state on failure.
	Activity = types.Activity

	// SavepointProvider is an optional extension of TxProvider for databases that support savepoints.
	// Implement this alongside TxProvider to enable rollback-to-stable on entry activity failures.
	SavepointProvider = types.SavepointProvider

	// TaskInvalidator is an optional extension of TaskStore for cancelling dangling tasks
	// when a hierarchical state subtree is exited. Implement this on your TaskStore to enable
	// automatic cancellation of pending tasks whose wait state is no longer active.
	TaskInvalidator = types.TaskInvalidator

	// Clock provides deterministic time for the engine.
	Clock = types.Clock

	// Guard is the interface for transition precondition checks.
	Guard = types.Guard

	// NamedGuard is an optional extension of Guard that provides a human-readable name.
	// Implement this on your guard structs to get meaningful names in observer events
	// instead of the default fmt.Sprintf("%T", guard) fallback.
	NamedGuard = types.NamedGuard

	// Observer is the marker type for all engine observer adapters.
	Observer = types.Observer

	// TransitionObserver receives a TransitionEvent after each successful state transition.
	TransitionObserver = types.TransitionObserver

	// GuardObserver receives a GuardFailureEvent when a guard rejects a transition.
	GuardObserver = types.GuardObserver

	// ActivityObserver receives events for activity lifecycle (dispatched, completed, failed).
	ActivityObserver = types.ActivityObserver

	// InfrastructureObserver receives StuckEvent and PostCommitErrorEvent.
	InfrastructureObserver = types.InfrastructureObserver
)

// Snapshot types — re-exported for the public API surface.
type (
	// Snapshot captures the complete runtime state of a workflow instance.
	Snapshot = types.Snapshot
)

// Observer event structs — re-exported for adapter implementors.
type (
	TransitionEvent       = types.TransitionEvent
	GuardFailureEvent     = types.GuardFailureEvent
	ActivityDispatchedEvent = types.ActivityDispatchedEvent
	ActivityCompletedEvent  = types.ActivityCompletedEvent
	ActivityFailedEvent     = types.ActivityFailedEvent
	StuckEvent            = types.StuckEvent
	PostCommitErrorEvent  = types.PostCommitErrorEvent
)

// RealClock uses time.Now().
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }
