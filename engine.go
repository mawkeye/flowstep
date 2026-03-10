package flowstep

import (
	"context"
	"fmt"

	internalruntime "github.com/mawkeye/flowstep/internal/runtime"
	"github.com/mawkeye/flowstep/types"
)

// Engine is the public workflow engine. Delegates to internal/engine.
type Engine struct {
	inner *internalruntime.Engine
}

// NewEngine creates a new Engine with the given options.
func NewEngine(opts ...Option) (*Engine, error) {
	cfg := &engineConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Apply defaults
	if cfg.clock == nil {
		cfg.clock = RealClock{}
	}
	if cfg.hooks == nil {
		cfg.hooks = NoopHooks{}
	}

	// Validate required dependencies
	if cfg.eventStore == nil {
		return nil, fmt.Errorf("flowstep: EventStore is required")
	}
	if cfg.instanceStore == nil {
		return nil, fmt.Errorf("flowstep: InstanceStore is required")
	}
	if cfg.txProvider == nil {
		return nil, fmt.Errorf("flowstep: TxProvider is required")
	}

	deps := internalruntime.Deps{
		EventStore:     cfg.eventStore,
		InstanceStore:  cfg.instanceStore,
		TaskStore:      cfg.taskStore,
		ChildStore:     cfg.childStore,
		ActivityStore:  cfg.activityStore,
		TxProvider:     cfg.txProvider,
		EventBus:       cfg.eventBus,
		ActivityRunner: cfg.activityRunner,
		Clock:          cfg.clock,
		Hooks:          cfg.hooks,

		ErrInstanceNotFound:  ErrInstanceNotFound,
		ErrInvalidTransition: ErrInvalidTransition,
		ErrAlreadyTerminal:   ErrAlreadyTerminal,
		ErrGuardFailed:       ErrGuardFailed,
		ErrNoMatchingSignal:  ErrNoMatchingSignal,
		ErrSignalAmbiguous:   ErrSignalAmbiguous,
		ErrNoMatchingRoute:   ErrNoMatchingRoute,
		ErrTaskNotFound:      ErrTaskNotFound,
		ErrInvalidChoice:     ErrInvalidChoice,
		ErrEngineShutdown:    ErrEngineShutdown,
	}

	return &Engine{inner: internalruntime.New(deps)}, nil
}

// Register adds a workflow definition to the engine.
func (e *Engine) Register(def *types.Definition) error {
	return e.inner.Register(def)
}

// Transition executes a named transition for the given aggregate.
func (e *Engine) Transition(
	ctx context.Context,
	aggregateType, aggregateID string,
	transitionName string,
	actorID string,
	params map[string]any,
) (*types.TransitionResult, error) {
	return e.inner.Transition(ctx, aggregateType, aggregateID, transitionName, actorID, params)
}

// Signal sends a signal to trigger a matching OnSignal transition.
func (e *Engine) Signal(ctx context.Context, input types.SignalInput) (*types.TransitionResult, error) {
	return e.inner.Signal(ctx, input)
}

// CompleteTask completes a pending task and fires the matching OnTaskCompleted transition.
func (e *Engine) CompleteTask(ctx context.Context, taskID, choice, actorID string) (*types.TransitionResult, error) {
	return e.inner.CompleteTask(ctx, taskID, choice, actorID)
}

// ChildCompleted notifies the parent workflow that a child has reached a terminal state.
func (e *Engine) ChildCompleted(ctx context.Context, childAggregateType, childAggregateID, terminalState string) (*types.TransitionResult, error) {
	return e.inner.ChildCompleted(ctx, childAggregateType, childAggregateID, terminalState)
}

// ForceState is an admin recovery operation that moves a workflow to any state,
// bypassing normal transition rules and guards.
func (e *Engine) ForceState(ctx context.Context, aggregateType, aggregateID, targetState, actorID, reason string) (*types.TransitionResult, error) {
	return e.inner.ForceState(ctx, aggregateType, aggregateID, targetState, actorID, reason)
}

// SideEffect executes fn exactly once and persists its result as a SideEffectEvent.
// On the first call, fn runs and the result is stored to the EventStore. Future replay
// (Task 9) will return the stored result without re-executing fn.
//
// At-least-once semantic: fn executes before the transaction commits. If Commit fails
// after fn has already run, fn may have had observable side effects. Design fn to be
// idempotent or accept at-least-once execution.
//
// Not safe to call from guards or hooks (they run inside a transition transaction).
func (e *Engine) SideEffect(ctx context.Context, aggregateType, aggregateID, name string, fn func() (any, error)) (any, error) {
	return e.inner.SideEffect(ctx, aggregateType, aggregateID, name, fn)
}

// Shutdown gracefully stops the engine. Waits for in-flight operations to complete.
func (e *Engine) Shutdown(ctx context.Context) error {
	return e.inner.Shutdown(ctx)
}
