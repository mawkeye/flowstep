package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mawkeye/flowstep/types"
)

// SideEffect executes fn exactly once and persists its result as a SideEffectEvent.
// On the first call, fn runs and the result is stored to EventStore within a transaction.
// This enables future replay (Task 9) to return the stored result without re-executing fn.
//
// At-least-once semantic: fn executes before the transaction commits. If Commit fails
// after fn has already run, fn may have had observable side effects. Design fn to be
// idempotent or accept at-least-once execution. Task 9 will upgrade this to exactly-once
// by checking for an existing SideEffectEvent before calling fn.
//
// Not safe to call from guards or hooks (they run inside a transition transaction).
// For use in activity implementations or external orchestration code.
func (e *Engine) SideEffect(ctx context.Context, aggregateType, aggregateID, name string, fn func() (any, error)) (any, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	// Verify aggregate exists before executing fn — prevents leaking side effects
	// for non-existent aggregates. Use InstanceStore.Get directly (not loadInstanceAndDef)
	// to avoid auto-creating an instance.
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: SideEffect get instance: %w", err)
	}

	// Execute fn after confirming the aggregate exists.
	result, err := fn()
	if err != nil {
		return nil, err
	}

	event := types.NewSideEffectEvent(
		generateID(),
		aggregateType, aggregateID,
		instance.WorkflowType, instance.WorkflowVersion,
		instance.CorrelationID,
		name, result,
		e.deps.Clock.Now(),
	)

	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("flowstep: SideEffect begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstep: SideEffect append event: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstep: SideEffect commit tx: %w", err)
	}

	return result, nil
}

// Transition executes a named transition for the given aggregate.
func (e *Engine) Transition(
	ctx context.Context,
	aggregateType, aggregateID string,
	transitionName string,
	actorID string,
	params map[string]any,
) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	start := e.deps.Clock.Now()

	def, instance, err := e.loadInstanceAndDef(ctx, aggregateType, aggregateID)
	if err != nil {
		return nil, err
	}

	tr, targetState, err := e.validateTransition(ctx, def, instance, transitionName, params)
	if err != nil {
		return nil, err
	}

	previousState := instance.CurrentState

	event, err := e.commitTransition(ctx, def, instance, tr, targetState, transitionName, actorID, params)
	if err != nil {
		return nil, err
	}

	return e.runPostCommit(ctx, def, instance, tr, event, previousState, params, start)
}

// loadInstanceAndDef retrieves or creates the workflow instance and resolves the matching definition.
func (e *Engine) loadInstanceAndDef(ctx context.Context, aggregateType, aggregateID string) (*types.Definition, *types.WorkflowInstance, error) {
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err != nil {
		if !errors.Is(err, e.deps.ErrInstanceNotFound) {
			return nil, nil, fmt.Errorf("flowstep: get instance: %w", err)
		}
		def, ok := e.definitionFor(aggregateType, 0)
		if !ok {
			return nil, nil, fmt.Errorf("flowstep: no workflow registered for aggregate type %q", aggregateType)
		}
		instance, err = e.createInstance(ctx, def, aggregateType, aggregateID)
		if err != nil {
			return nil, nil, err
		}
		return def, instance, nil
	}
	def, ok := e.definitionFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, aggregateType)
	}
	return def, instance, nil
}

// validateTransition validates the transition is allowed from the instance's current state,
// runs guards, and resolves the target state. Returns the transition definition and target state.
func (e *Engine) validateTransition(
	ctx context.Context,
	def *types.Definition,
	instance *types.WorkflowInstance,
	transitionName string,
	params map[string]any,
) (types.TransitionDef, string, error) {
	// 1. Look up transition
	tr, ok := def.Transitions[transitionName]
	if !ok {
		return types.TransitionDef{}, "", fmt.Errorf("flowstep: transition %q not found in workflow %q: %w",
			transitionName, def.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// 2. Check if already terminal
	if st, exists := def.States[instance.CurrentState]; exists && st.IsTerminal {
		return types.TransitionDef{}, "", fmt.Errorf("flowstep: workflow %s/%s is in terminal state %q: %w",
			instance.AggregateType, instance.AggregateID, instance.CurrentState, e.deps.ErrAlreadyTerminal)
	}

	// 3. Validate source state
	if !slices.Contains(tr.Sources, instance.CurrentState) {
		return types.TransitionDef{}, "", fmt.Errorf("flowstep: transition %q not valid from state %q (expected one of %v): %w",
			transitionName, instance.CurrentState, tr.Sources, e.deps.ErrInvalidTransition)
	}

	// 4. Run guards
	if err := e.runGuards(ctx, def.WorkflowType, tr, instance, params); err != nil {
		return types.TransitionDef{}, "", err
	}

	// 5. Determine target state (direct or routed)
	targetState := tr.Target
	if len(tr.Routes) > 0 {
		resolved, err := e.resolveRoute(ctx, tr, instance, params)
		if err != nil {
			return types.TransitionDef{}, "", err
		}
		targetState = resolved
	}

	return tr, targetState, nil
}

// commitTransition builds the domain event, mutates the instance, and persists both in a transaction.
func (e *Engine) commitTransition(
	ctx context.Context,
	def *types.Definition,
	instance *types.WorkflowInstance,
	tr types.TransitionDef,
	targetState, transitionName, actorID string,
	params map[string]any,
) (types.DomainEvent, error) {
	// 1. Build event
	now := e.deps.Clock.Now()
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   instance.AggregateType,
		AggregateID:     instance.AggregateID,
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

	// 2. Mutate instance — snapshot UpdatedAt for optimistic locking before overwriting
	instance.LastReadUpdatedAt = instance.UpdatedAt
	instance.CurrentState = targetState
	instance.UpdatedAt = now

	// 3. Persist event + updated instance in a transaction
	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstep: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: commit tx: %w", err)
	}

	return event, nil
}

// createInstance creates a new workflow instance in a transaction.
func (e *Engine) createInstance(
	ctx context.Context,
	def *types.Definition,
	aggregateType, aggregateID string,
) (*types.WorkflowInstance, error) {
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
		return nil, fmt.Errorf("flowstep: begin tx for create: %w", err)
	}

	if err := e.deps.InstanceStore.Create(ctx, tx, newInstance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstep: create instance: %w", err)
	}

	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstep: commit create: %w", err)
	}

	return &newInstance, nil
}

// ForceState is an admin recovery operation that bypasses normal transition rules.
// It moves a workflow instance to any state, including from terminal states.
func (e *Engine) ForceState(ctx context.Context, aggregateType, aggregateID, targetState, actorID, reason string) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	// Load instance (must exist)
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get instance: %w", err)
	}

	// Look up definition for instance's version
	def, ok := e.definitionFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, aggregateType)
	}

	// Validate target state exists in definition
	if _, exists := def.States[targetState]; !exists {
		return nil, fmt.Errorf("flowstep: target state %q not found in workflow %q: %w",
			targetState, def.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// Build event
	now := e.deps.Clock.Now()
	previousState := instance.CurrentState
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		WorkflowType:    def.WorkflowType,
		WorkflowVersion: def.Version,
		EventType:       "StateForced",
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  "_force_state",
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         map[string]any{"_reason": reason, "_from": previousState, "_to": targetState},
		CreatedAt:       now,
	}

	// Update instance
	instance.LastReadUpdatedAt = instance.UpdatedAt // snapshot for optimistic locking
	instance.CurrentState = targetState
	instance.IsStuck = false
	instance.StuckReason = ""
	instance.UpdatedAt = now

	// Transaction: persist
	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("flowstep: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstep: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstep: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstep: commit tx: %w", err)
	}

	// Post-commit: emit event
	var forceWarnings []types.PostCommitWarning
	if e.deps.EventBus != nil {
		if emitErr := e.deps.EventBus.Emit(ctx, event); emitErr != nil {
			forceWarnings = append(forceWarnings, types.PostCommitWarning{Operation: "EventBus.Emit", Err: emitErr})
			e.deps.Hooks.OnPostCommitError(ctx, "EventBus.Emit", emitErr)
		}
	}

	isTerminal := false
	if st, exists := def.States[targetState]; exists && st.IsTerminal {
		isTerminal = true
	}

	return &types.TransitionResult{
		Instance:       *instance,
		Event:          event,
		PreviousState:  previousState,
		NewState:       targetState,
		TransitionName: "_force_state",
		IsTerminal:     isTerminal,
		Warnings:       forceWarnings,
	}, nil
}

