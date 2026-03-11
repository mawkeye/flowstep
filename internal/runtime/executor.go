package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mawkeye/flowstep/internal/graph"
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

	cm, instance, err := e.loadInstanceAndCompiled(ctx, aggregateType, aggregateID)
	if err != nil {
		return nil, err
	}

	ct, targetState, err := e.validateTransition(ctx, cm, instance, transitionName, params)
	if err != nil {
		return nil, err
	}

	previousState := instance.CurrentState

	event, err := e.commitTransition(ctx, cm, instance, ct, targetState, transitionName, actorID, params)
	if err != nil {
		return nil, err
	}

	return e.runPostCommit(ctx, cm, instance, ct, event, previousState, params, start)
}

// loadInstanceAndCompiled retrieves or creates the workflow instance and resolves the matching compiled machine.
func (e *Engine) loadInstanceAndCompiled(ctx context.Context, aggregateType, aggregateID string) (*graph.CompiledMachine, *types.WorkflowInstance, error) {
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	if err != nil {
		if !errors.Is(err, e.deps.ErrInstanceNotFound) {
			return nil, nil, fmt.Errorf("flowstep: get instance: %w", err)
		}
		cm, ok := e.compiledFor(aggregateType, 0)
		if !ok {
			return nil, nil, fmt.Errorf("flowstep: no workflow registered for aggregate type %q", aggregateType)
		}
		instance, err = e.createInstance(ctx, cm, aggregateType, aggregateID)
		if err != nil {
			return nil, nil, err
		}
		return cm, instance, nil
	}
	cm, ok := e.compiledFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, aggregateType)
	}
	return cm, instance, nil
}

// validateTransition validates the transition is allowed from the instance's current state,
// runs guards, and resolves the target state. Returns the compiled transition and target state.
func (e *Engine) validateTransition(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	transitionName string,
	params map[string]any,
) (*graph.CompiledTransition, string, error) {
	// 1. Look up transition by name in the definition
	tr, ok := cm.Definition.Transitions[transitionName]
	if !ok {
		return nil, "", fmt.Errorf("flowstep: transition %q not found in workflow %q: %w",
			transitionName, cm.Definition.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// 2. Check if already terminal
	if st, exists := cm.Definition.States[instance.CurrentState]; exists && st.IsTerminal {
		return nil, "", fmt.Errorf("flowstep: workflow %s/%s is in terminal state %q: %w",
			instance.AggregateType, instance.AggregateID, instance.CurrentState, e.deps.ErrAlreadyTerminal)
	}

	// 3. Validate source state — check leaf first, then bubble up through ancestors.
	effectiveSource := instance.CurrentState
	if !slices.Contains(tr.Sources, instance.CurrentState) {
		// Event bubbling: walk ancestors nearest-first; first match wins.
		found := false
		for _, ancestor := range cm.Ancestry[instance.CurrentState] {
			if slices.Contains(tr.Sources, ancestor) {
				effectiveSource = ancestor
				found = true
				break
			}
		}
		if !found {
			return nil, "", fmt.Errorf("flowstep: transition %q not valid from state %q (expected one of %v): %w",
				transitionName, instance.CurrentState, tr.Sources, e.deps.ErrInvalidTransition)
		}
	}

	// 4. Find the compiled transition (with precomputed guard names) using effectiveSource.
	var ct *graph.CompiledTransition
	for _, c := range cm.TransitionsByState[effectiveSource] {
		if c.Def.Name == transitionName {
			ct = c
			break
		}
	}
	if ct == nil {
		// Fallback: should not happen after Compile() succeeds, but defend against it.
		return nil, "", fmt.Errorf("flowstep: compiled transition %q missing for state %q: %w",
			transitionName, effectiveSource, e.deps.ErrInvalidTransition)
	}

	// 5. Run guards using precomputed names
	if err := e.runGuards(ctx, cm.Definition.WorkflowType, ct, instance, params); err != nil {
		return nil, "", err
	}

	// 6. Determine target state (direct or routed)
	targetState := tr.Target
	if len(tr.Routes) > 0 {
		resolved, err := e.resolveRoute(ctx, tr, instance, params)
		if err != nil {
			return nil, "", err
		}
		targetState = resolved
	}

	return ct, targetState, nil
}

// commitTransition builds the domain event, runs exit/entry activities, mutates the instance,
// and persists both in a transaction.
func (e *Engine) commitTransition(
	ctx context.Context,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
	ct *graph.CompiledTransition,
	targetState, transitionName, actorID string,
	params map[string]any,
) (types.DomainEvent, error) {
	sourceLeaf := instance.CurrentState
	now := e.deps.Clock.Now()

	// 1. Resolve target state — history-aware if HistoryMode is set, otherwise InitialLeafMap fallback.
	resolvedTarget := targetState
	switch ct.Def.HistoryMode {
	case types.HistoryShallow:
		if recorded, ok := instance.ShallowHistory[targetState]; ok {
			resolvedTarget = recorded
			// Secondary resolution: if the shallow-recorded child is itself compound,
			// resolve it to its initial leaf via InitialLeafMap.
			// Note: secondary resolution always uses InitialLeafMap (not the child
			// compound's own history). Only the transition marked WithHistory(Shallow)
			// uses history; compound children of the resolved target use their InitialChild.
			if leaf, ok2 := cm.InitialLeafMap[resolvedTarget]; ok2 {
				resolvedTarget = leaf
			}
		} else if leaf, ok := cm.InitialLeafMap[targetState]; ok {
			resolvedTarget = leaf // no history yet — fall back to initial leaf
		}
	case types.HistoryDeep:
		if recorded, ok := instance.DeepHistory[targetState]; ok {
			resolvedTarget = recorded // deep history always points to a leaf
		} else if leaf, ok := cm.InitialLeafMap[targetState]; ok {
			resolvedTarget = leaf // no history yet — fall back to initial leaf
		}
	default:
		if leaf, ok := cm.InitialLeafMap[targetState]; ok {
			resolvedTarget = leaf
		}
	}

	// 2. Build event (ID used as causation metadata for activities).
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   instance.AggregateType,
		AggregateID:     instance.AggregateID,
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		EventType:       ct.Def.Event,
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  transitionName,
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         params,
		CreatedAt:       now,
	}

	// 3. Compute exit/entry sequences.
	lca, _ := cm.LCA(sourceLeaf, resolvedTarget)
	exitSeq := computeExitSequence(sourceLeaf, lca, cm.Ancestry)
	entrySeq := computeEntrySequence(lca, resolvedTarget, cm.Ancestry)

	// 3b. Record history for all compound states in the exit sequence.
	// Must happen after exitSeq is computed, before tx.Begin, so that a tx rollback
	// on exit activity failure leaves history unrecorded (exit was not completed).
	// Uses copy-on-write to avoid aliasing the stored map in memstore.
	recordHistory(instance, sourceLeaf, exitSeq, cm.Definition)

	actInput := types.ActivityInput{
		WorkflowType:  cm.Definition.WorkflowType,
		AggregateType: instance.AggregateType,
		AggregateID:   instance.AggregateID,
		CorrelationID: instance.CorrelationID,
		Transition:    transitionName,
		SourceState:   sourceLeaf,
		EventID:       event.ID,
		Params:        params,
	}

	// 4. Begin transaction.
	tx, err := e.deps.TxProvider.Begin(ctx)
	if err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstep: begin tx: %w", err)
	}

	// 5. Execute exit activities (abort transition on any failure via full rollback).
	// Savepoints are not used here because exit failure always rolls back the entire transaction.
	for _, stateName := range exitSeq {
		st := cm.Definition.States[stateName]
		if st.ExitActivity == "" {
			continue
		}
		if err := e.runSyncActivity(ctx, st.ExitActivity, actInput); err != nil {
			_ = e.deps.TxProvider.Rollback(ctx, tx)
			return types.DomainEvent{}, fmt.Errorf("flowstep: exit activity %q failed for state %q: %w",
				st.ExitActivity, stateName, err)
		}
	}

	// 6. Execute entry activities (STUCK on failure).
	stuck := false
	var stuckReason string
	for _, stateName := range entrySeq {
		st := cm.Definition.States[stateName]
		if st.EntryActivity == "" {
			continue
		}
		if e.hasSavepoints {
			sp := e.deps.TxProvider.(types.SavepointProvider)
			_ = sp.Savepoint(ctx, tx, "entry_"+stateName)
		}
		if err := e.runSyncActivity(ctx, st.EntryActivity, actInput); err != nil {
			stuckReason = fmt.Sprintf("entry activity %q failed for state %q: %v",
				st.EntryActivity, stateName, err)
			if e.hasSavepoints {
				sp := e.deps.TxProvider.(types.SavepointProvider)
				_ = sp.RollbackTo(ctx, tx, "entry_"+stateName)
				stuck = true
				break
			}
			// Without savepoints: rollback tx, mark STUCK in a new transaction.
			_ = e.deps.TxProvider.Rollback(ctx, tx)
			instance.LastReadUpdatedAt = instance.UpdatedAt
			instance.CurrentState = sourceLeaf
			instance.IsStuck = true
			instance.StuckReason = stuckReason
			instance.UpdatedAt = now
			if stuckTx, txErr := e.deps.TxProvider.Begin(ctx); txErr == nil {
				_ = e.deps.InstanceStore.Update(ctx, stuckTx, *instance)
				_ = e.deps.TxProvider.Commit(ctx, stuckTx)
			}
			return types.DomainEvent{}, fmt.Errorf("flowstep: %s", stuckReason)
		}
	}

	// 7. Invalidate dangling tasks for all exited states (TaskInvalidator — optional).
	if e.deps.TaskStore != nil {
		if inv, ok := e.deps.TaskStore.(types.TaskInvalidator); ok {
			exitedStates := collectSubtreeStates(exitSeq, cm.Definition)
			if len(exitedStates) > 0 {
				if invErr := inv.InvalidateByStates(ctx, tx, instance.AggregateType, instance.AggregateID, exitedStates); invErr != nil {
					e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "TaskInvalidator.InvalidateByStates", Err: invErr})
				}
			}
		}
	}

	// 8. Mutate instance — snapshot UpdatedAt for optimistic locking before overwriting.
	instance.LastReadUpdatedAt = instance.UpdatedAt
	if stuck {
		instance.CurrentState = sourceLeaf
		instance.IsStuck = true
		instance.StuckReason = stuckReason
	} else {
		instance.CurrentState = resolvedTarget
	}
	instance.UpdatedAt = now

	// 9. Persist event + updated instance in a transaction.
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

	if stuck {
		return event, fmt.Errorf("flowstep: %s", stuckReason)
	}
	return event, nil
}

// createInstance creates a new workflow instance in a transaction.
func (e *Engine) createInstance(
	ctx context.Context,
	cm *graph.CompiledMachine,
	aggregateType, aggregateID string,
) (*types.WorkflowInstance, error) {
	now := e.deps.Clock.Now()
	initialState := cm.Definition.InitialState
	// If InitialState is compound, resolve to the initial leaf state.
	if leaf, ok := cm.InitialLeafMap[initialState]; ok {
		initialState = leaf
	}
	newInstance := types.WorkflowInstance{
		ID:              generateID(),
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		CurrentState:    initialState,
		StateData:       make(map[string]any),
		ShallowHistory:  make(map[string]string),
		DeepHistory:     make(map[string]string),
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

	// Look up compiled machine for instance's version
	cm, ok := e.compiledFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, aggregateType)
	}

	// Validate target state exists in definition
	if _, exists := cm.Definition.States[targetState]; !exists {
		return nil, fmt.Errorf("flowstep: target state %q not found in workflow %q: %w",
			targetState, cm.Definition.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// Resolve compound target to its initial leaf.
	if leaf, ok := cm.InitialLeafMap[targetState]; ok {
		targetState = leaf
	}

	// Build event
	now := e.deps.Clock.Now()
	previousState := instance.CurrentState
	event := types.DomainEvent{
		ID:              generateID(),
		AggregateType:   aggregateType,
		AggregateID:     aggregateID,
		WorkflowType:    cm.Definition.WorkflowType,
		WorkflowVersion: cm.Definition.Version,
		EventType:       "StateForced",
		CorrelationID:   instance.CorrelationID,
		ActorID:         actorID,
		TransitionName:  "_force_state",
		StateBefore:     copyMap(instance.StateData),
		StateAfter:      copyMap(instance.StateData),
		Payload:         map[string]any{"_reason": reason, "_from": previousState, "_to": targetState},
		CreatedAt:       now,
	}

	// Update instance — clear history maps (admin recovery resets state from scratch).
	instance.LastReadUpdatedAt = instance.UpdatedAt // snapshot for optimistic locking
	instance.CurrentState = targetState
	instance.IsStuck = false
	instance.StuckReason = ""
	instance.ShallowHistory = make(map[string]string)
	instance.DeepHistory = make(map[string]string)
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
			e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "EventBus.Emit", Err: emitErr})
		}
	}

	isTerminal := false
	if st, exists := cm.Definition.States[targetState]; exists && st.IsTerminal {
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

