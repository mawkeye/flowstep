package runtime

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/mawkeye/flowstep/internal/graph"
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
	Observers      *ObserverRegistry

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

	// Graph compilation sentinels — passed through to graph.Validate / graph.DetectSpawnCycles
	Sentinels graph.Sentinels
}

// Engine executes workflow state transitions.
type Engine struct {
	deps     Deps
	versions map[string]map[int]*types.Definition // key: aggregateType -> version -> def
	latest   map[string]*types.Definition         // key: aggregateType -> latest def

	compiled       map[string]map[int]*graph.CompiledMachine // key: aggregateType -> version -> cm
	latestCompiled map[string]*graph.CompiledMachine         // key: aggregateType -> latest cm

	// hasSavepoints is true when deps.TxProvider also implements types.SavepointProvider.
	// Detected once in New() via type assertion.
	hasSavepoints bool

	mu       sync.RWMutex
	shutdown atomic.Bool
	wg       sync.WaitGroup
}

// New creates a new Engine with the given dependencies.
func New(deps Deps) *Engine {
	_, hasSP := deps.TxProvider.(types.SavepointProvider)
	return &Engine{
		deps:           deps,
		versions:       make(map[string]map[int]*types.Definition),
		latest:         make(map[string]*types.Definition),
		compiled:       make(map[string]map[int]*graph.CompiledMachine),
		latestCompiled: make(map[string]*graph.CompiledMachine),
		hasSavepoints:  hasSP,
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

// Register validates, compiles, and adds a workflow definition to the engine.
// It also detects cross-workflow spawn cycles across all registered definitions.
func (e *Engine) Register(def *types.Definition) error {
	if err := graph.Validate(def, e.deps.Sentinels); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Hash dedup: skip recompilation when the same definition content is re-registered.
	if existing, ok := e.compiled[def.AggregateType][def.Version]; ok {
		newHash := graph.HashDefinition(def)
		if existing.DefinitionHash == newHash {
			return nil
		}
	}

	cm, err := graph.Compile(def, e.deps.Sentinels)
	if err != nil {
		return err
	}

	// Build candidate allDefs (existing registered definitions + new def) WITHOUT
	// writing to maps yet. DetectSpawnCycles must run before any map mutation so
	// that a rejected definition never pollutes the registry.
	allDefs := make([]*types.Definition, 0)
	for _, versions := range e.versions {
		for _, d := range versions {
			allDefs = append(allDefs, d)
		}
	}
	allDefs = append(allDefs, def)
	if err := graph.DetectSpawnCycles(allDefs, e.deps.Sentinels); err != nil {
		return err
	}

	// Cycle-free: commit the definition and compiled machine to the registry.
	if _, ok := e.versions[def.AggregateType]; !ok {
		e.versions[def.AggregateType] = make(map[int]*types.Definition)
	}
	e.versions[def.AggregateType][def.Version] = def
	if current, ok := e.latest[def.AggregateType]; !ok || def.Version > current.Version {
		e.latest[def.AggregateType] = def
	}

	if _, ok := e.compiled[def.AggregateType]; !ok {
		e.compiled[def.AggregateType] = make(map[int]*graph.CompiledMachine)
	}
	e.compiled[def.AggregateType][def.Version] = cm
	if current, ok := e.latestCompiled[def.AggregateType]; !ok || def.Version > current.Definition.Version {
		e.latestCompiled[def.AggregateType] = cm
	}

	return nil
}

// compiledFor returns the compiled machine for the given aggregate type and version.
// If version is 0, returns the latest compiled machine.
func (e *Engine) compiledFor(aggregateType string, version int) (*graph.CompiledMachine, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if version == 0 {
		cm, ok := e.latestCompiled[aggregateType]
		return cm, ok
	}
	versions, ok := e.compiled[aggregateType]
	if !ok {
		return nil, false
	}
	cm, ok := versions[version]
	return cm, ok
}
