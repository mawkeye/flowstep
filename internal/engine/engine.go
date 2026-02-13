package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/mawkeye/flowstate/types"
)

// Deps holds all external dependencies injected by the root package.
// This avoids the internal engine importing the root package.
type Deps struct {
	EventStore     EventStore
	InstanceStore  InstanceStore
	ActivityStore  ActivityStore
	TxProvider     TxProvider
	EventBus       EventBus
	ActivityRunner ActivityRunner
	Clock          Clock
	Hooks          Hooks

	// Sentinel errors from root package
	ErrInstanceNotFound  error
	ErrInvalidTransition error
	ErrAlreadyTerminal   error
	ErrGuardFailed       error
}

// EventStore interface (mirrors root, avoids import cycle).
type EventStore interface {
	Append(ctx context.Context, tx any, event types.DomainEvent) error
	ListByCorrelation(ctx context.Context, correlationID string) ([]types.DomainEvent, error)
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.DomainEvent, error)
}

// InstanceStore interface.
type InstanceStore interface {
	Get(ctx context.Context, aggregateType, aggregateID string) (*types.WorkflowInstance, error)
	Create(ctx context.Context, tx any, instance types.WorkflowInstance) error
	Update(ctx context.Context, tx any, instance types.WorkflowInstance) error
	ListStuck(ctx context.Context) ([]types.WorkflowInstance, error)
}

// TxProvider interface.
type TxProvider interface {
	Begin(ctx context.Context) (tx any, err error)
	Commit(ctx context.Context, tx any) error
	Rollback(ctx context.Context, tx any) error
}

// EventBus interface.
type EventBus interface {
	Emit(ctx context.Context, event types.DomainEvent) error
}

// ActivityStore interface.
type ActivityStore interface {
	Create(ctx context.Context, tx any, invocation types.ActivityInvocation) error
	Get(ctx context.Context, invocationID string) (*types.ActivityInvocation, error)
	UpdateStatus(ctx context.Context, invocationID, status string, result *types.ActivityResult) error
	ListByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.ActivityInvocation, error)
}

// ActivityRunner interface.
type ActivityRunner interface {
	Dispatch(ctx context.Context, invocation types.ActivityInvocation) error
}

// Clock interface.
type Clock interface {
	Now() time.Time
}

// Hooks interface.
type Hooks interface {
	OnTransition(ctx context.Context, result types.TransitionResult, duration time.Duration)
	OnGuardFailed(ctx context.Context, workflowType, transitionName, guardName string, err error)
	OnStuck(ctx context.Context, instance types.WorkflowInstance, reason string)
}

// Engine executes workflow state transitions.
type Engine struct {
	deps        Deps
	definitions map[string]*types.Definition // key: aggregateType
}

// New creates a new Engine with the given dependencies.
func New(deps Deps) *Engine {
	return &Engine{
		deps:        deps,
		definitions: make(map[string]*types.Definition),
	}
}

// Register adds a workflow definition to the engine.
func (e *Engine) Register(def *types.Definition) {
	e.definitions[def.AggregateType] = def
}

// Transition executes a named transition for the given aggregate.
func (e *Engine) Transition(
	ctx context.Context,
	aggregateType, aggregateID string,
	transitionName string,
	actorID string,
	params map[string]any,
) (*types.TransitionResult, error) {
	start := e.deps.Clock.Now()

	// 1. Look up definition
	def, ok := e.definitions[aggregateType]
	if !ok {
		return nil, fmt.Errorf("flowstate: no workflow registered for aggregate type %q", aggregateType)
	}

	// 2. Look up transition
	tr, ok := def.Transitions[transitionName]
	if !ok {
		return nil, fmt.Errorf("flowstate: transition %q not found in workflow %q: %w",
			transitionName, def.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// 3. Load or create workflow instance
	instance, err := e.loadOrCreate(ctx, def, aggregateType, aggregateID)
	if err != nil {
		return nil, err
	}

	// 4. Check if already terminal
	if st, exists := def.States[instance.CurrentState]; exists && st.IsTerminal {
		return nil, fmt.Errorf("flowstate: workflow %s/%s is in terminal state %q: %w",
			aggregateType, aggregateID, instance.CurrentState, e.deps.ErrAlreadyTerminal)
	}

	// 5. Validate source state
	if !containsSource(tr.Sources, instance.CurrentState) {
		return nil, fmt.Errorf("flowstate: transition %q not valid from state %q (expected one of %v): %w",
			transitionName, instance.CurrentState, tr.Sources, e.deps.ErrInvalidTransition)
	}

	// 6. Run guards
	if err := e.runGuards(ctx, tr, nil, params); err != nil {
		return nil, err
	}

	// 7. Determine target state
	targetState := tr.Target

	// 8. Build event
	now := e.deps.Clock.Now()
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		WorkflowType:    def.WorkflowType,
		WorkflowVersion: def.Version,
		EventType:       tr.Event,
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  transitionName,
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         params,
		CreatedAt:       now,
	}

	// 9. Update instance
	previousState := instance.CurrentState
	oldUpdatedAt := instance.UpdatedAt
	instance.CurrentState = targetState
	instance.UpdatedAt = now

	// 10. Transaction: persist event + update instance
	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("flowstate: begin tx: %w", err)
	}

	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstate: append event: %w", err)
	}

	// For new instances, create; for existing, update with optimistic lock
	if previousState == def.InitialState && oldUpdatedAt.Equal(instance.CreatedAt) {
		// This was just created by loadOrCreate — update it
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstate: update instance: %w", err)
	}

	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstate: commit tx: %w", err)
	}

	// 11. Post-commit: emit event
	if e.deps.EventBus != nil {
		_ = e.deps.EventBus.Emit(ctx, event)
	}

	// 12. Post-commit: dispatch activities
	var activitiesDispatched []string
	if len(tr.Activities) > 0 && e.deps.ActivityRunner != nil {
		for _, actDef := range tr.Activities {
			invocation := types.ActivityInvocation{
				ID:            generateID(),
				ActivityName:  actDef.Name,
				WorkflowType:  def.WorkflowType,
				AggregateType: aggregateType,
				AggregateID:   aggregateID,
				CorrelationID: instance.CorrelationID,
				Mode:          actDef.Mode,
				Input: types.ActivityInput{
					WorkflowType:  def.WorkflowType,
					AggregateType: aggregateType,
					AggregateID:   aggregateID,
					CorrelationID: instance.CorrelationID,
					Params:        params,
					ScheduledAt:   now,
				},
				RetryPolicy: actDef.RetryPolicy,
				Timeout:     actDef.Timeout,
				Status:      types.ActivityStatusScheduled,
				MaxAttempts:  1,
				ScheduledAt: now,
			}

			if actDef.RetryPolicy != nil {
				invocation.MaxAttempts = actDef.RetryPolicy.MaxAttempts
			}

			// Store invocation
			if e.deps.ActivityStore != nil {
				_ = e.deps.ActivityStore.Create(ctx, nil, invocation)
			}

			// Dispatch to runner
			_ = e.deps.ActivityRunner.Dispatch(ctx, invocation)
			activitiesDispatched = append(activitiesDispatched, actDef.Name)
		}
	}

	// 13. Build result
	isTerminal := false
	if st, exists := def.States[targetState]; exists && st.IsTerminal {
		isTerminal = true
	}

	result := &types.TransitionResult{
		Instance:             *instance,
		Event:                event,
		PreviousState:        previousState,
		NewState:             targetState,
		TransitionName:       transitionName,
		ActivitiesDispatched: activitiesDispatched,
		IsTerminal:           isTerminal,
	}

	// 13. Post-commit: hooks
	duration := e.deps.Clock.Now().Sub(start)
	e.deps.Hooks.OnTransition(ctx, *result, duration)

	return result, nil
}

func (e *Engine) loadOrCreate(
	ctx context.Context,
	def *types.Definition,
	aggregateType, aggregateID string,
) (*types.WorkflowInstance, error) {
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err == nil {
		return instance, nil
	}

	if !errors.Is(err, e.deps.ErrInstanceNotFound) {
		return nil, fmt.Errorf("flowstate: get instance: %w", err)
	}

	// Auto-create at initial state
	now := e.deps.Clock.Now()
	newInstance := types.WorkflowInstance{
		ID:              generateID(),
		WorkflowType:    def.WorkflowType,
		WorkflowVersion: def.Version,
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		CurrentState:    def.InitialState,
		StateData:       make(map[string]any),
		CorrelationID:   generateID(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("flowstate: begin tx for create: %w", err)
	}

	if err := e.deps.InstanceStore.Create(ctx, tx, newInstance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstate: create instance: %w", err)
	}

	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstate: commit create: %w", err)
	}

	return &newInstance, nil
}

func (e *Engine) runGuards(ctx context.Context, tr types.TransitionDef, aggregate any, params map[string]any) error {
	for _, guard := range tr.Guards {
		if err := guard.Check(ctx, aggregate, params); err != nil {
			return fmt.Errorf("flowstate: guard failed: %w", e.deps.ErrGuardFailed)
		}
	}
	return nil
}

func containsSource(sources []string, state string) bool {
	for _, s := range sources {
		if s == state {
			return true
		}
	}
	return false
}

func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
