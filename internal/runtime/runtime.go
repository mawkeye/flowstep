package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mawkeye/flowstep/types"
)

// Deps holds all external dependencies injected by the root package.
// All interface types are defined in types/ to avoid circular imports.
type Deps struct {
	EventStore     types.EventStore
	InstanceStore  types.InstanceStore
	TaskStore      types.TaskStore
	ChildStore     types.ChildStore
	ActivityStore  types.ActivityStore
	TxProvider     types.TxProvider
	EventBus       types.EventBus
	ActivityRunner types.ActivityRunner
	Clock          types.Clock
	Hooks          types.Hooks

	// Sentinel errors from root package
	ErrInstanceNotFound  error
	ErrInvalidTransition error
	ErrAlreadyTerminal   error
	ErrGuardFailed       error
	ErrNoMatchingSignal  error
	ErrSignalAmbiguous   error
	ErrNoMatchingRoute   error
	ErrTaskNotFound      error
	ErrInvalidChoice     error
	ErrEngineShutdown    error
}

// Engine executes workflow state transitions.
type Engine struct {
	deps     Deps
	versions map[string]map[int]*types.Definition // key: aggregateType -> version -> def
	latest   map[string]*types.Definition         // key: aggregateType -> latest def

	mu       sync.RWMutex
	shutdown atomic.Bool
	wg       sync.WaitGroup
}

// New creates a new Engine with the given dependencies.
func New(deps Deps) *Engine {
	return &Engine{
		deps:     deps,
		versions: make(map[string]map[int]*types.Definition),
		latest:   make(map[string]*types.Definition),
	}
}

// Shutdown gracefully stops the engine. Waits for in-flight operations to complete,
// or returns ctx.Err() if the context is cancelled first.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.shutdown.Store(true)
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) checkShutdown() error {
	if e.shutdown.Load() {
		return fmt.Errorf("flowstep: engine is shut down: %w", e.deps.ErrEngineShutdown)
	}
	return nil
}

// Register adds a workflow definition to the engine.
func (e *Engine) Register(def *types.Definition) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.versions[def.AggregateType]; !ok {
		e.versions[def.AggregateType] = make(map[int]*types.Definition)
	}
	e.versions[def.AggregateType][def.Version] = def

	// Track latest version (highest version number)
	if current, ok := e.latest[def.AggregateType]; !ok || def.Version > current.Version {
		e.latest[def.AggregateType] = def
	}
	return nil
}

// definitionFor returns the definition for the given aggregate type and version.
// If version is 0, returns the latest registered definition.
func (e *Engine) definitionFor(aggregateType string, version int) (*types.Definition, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if version == 0 {
		def, ok := e.latest[aggregateType]
		return def, ok
	}
	versions, ok := e.versions[aggregateType]
	if !ok {
		return nil, false
	}
	def, ok := versions[version]
	return def, ok
}
