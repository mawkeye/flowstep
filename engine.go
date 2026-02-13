package flowstate

import (
	"context"
	"fmt"

	internalengine "github.com/mawkeye/flowstate/internal/engine"
	"github.com/mawkeye/flowstate/types"
)

// Engine is the public workflow engine. Delegates to internal/engine.
type Engine struct {
	inner *internalengine.Engine
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
		return nil, fmt.Errorf("flowstate: EventStore is required")
	}
	if cfg.instanceStore == nil {
		return nil, fmt.Errorf("flowstate: InstanceStore is required")
	}
	if cfg.txProvider == nil {
		return nil, fmt.Errorf("flowstate: TxProvider is required")
	}

	deps := internalengine.Deps{
		EventStore:     cfg.eventStore,
		InstanceStore:  cfg.instanceStore,
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
	}

	return &Engine{inner: internalengine.New(deps)}, nil
}

// Register adds a workflow definition to the engine.
func (e *Engine) Register(def *types.Definition) {
	e.inner.Register(def)
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
