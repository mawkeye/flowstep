package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mawkeye/flowstate/types"
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
	go func() { e.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Engine) checkShutdown() error {
	if e.shutdown.Load() {
		return fmt.Errorf("flowstate: engine is shut down: %w", e.deps.ErrEngineShutdown)
	}
	return nil
}

// Register adds a workflow definition to the engine.
// If a definition with the same aggregate type already exists, the newer version
// becomes the default for new instances. Existing instances continue using their
// creation version.
func (e *Engine) Register(def *types.Definition) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, ok := e.versions[def.AggregateType]; !ok {
		e.versions[def.AggregateType] = make(map[int]*types.Definition)
	}
	e.versions[def.AggregateType][def.Version] = def

	// Update latest if this version is higher
	if cur, ok := e.latest[def.AggregateType]; !ok || def.Version >= cur.Version {
		e.latest[def.AggregateType] = def
	}
	return nil
}

// definitionFor returns the definition for the given aggregate type and version.
// If version is 0, returns the latest version.
// Callers do not need to hold any lock — this method acquires RLock internally.
func (e *Engine) definitionFor(aggregateType string, version int) (*types.Definition, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if version > 0 {
		if versions, ok := e.versions[aggregateType]; ok {
			def, ok := versions[version]
			return def, ok
		}
		return nil, false
	}
	def, ok := e.latest[aggregateType]
	return def, ok
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
			return nil, nil, fmt.Errorf("flowstate: get instance: %w", err)
		}
		def, ok := e.definitionFor(aggregateType, 0)
		if !ok {
			return nil, nil, fmt.Errorf("flowstate: no workflow registered for aggregate type %q", aggregateType)
		}
		instance, err = e.createInstance(ctx, def, aggregateType, aggregateID)
		if err != nil {
			return nil, nil, err
		}
		return def, instance, nil
	}
	def, ok := e.definitionFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, nil, fmt.Errorf("flowstate: no workflow version %d registered for aggregate type %q",
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
		return types.TransitionDef{}, "", fmt.Errorf("flowstate: transition %q not found in workflow %q: %w",
			transitionName, def.WorkflowType, e.deps.ErrInvalidTransition)
	}

	// 2. Check if already terminal
	if st, exists := def.States[instance.CurrentState]; exists && st.IsTerminal {
		return types.TransitionDef{}, "", fmt.Errorf("flowstate: workflow %s/%s is in terminal state %q: %w",
			instance.AggregateType, instance.AggregateID, instance.CurrentState, e.deps.ErrAlreadyTerminal)
	}

	// 3. Validate source state
	if !slices.Contains(tr.Sources, instance.CurrentState) {
		return types.TransitionDef{}, "", fmt.Errorf("flowstate: transition %q not valid from state %q (expected one of %v): %w",
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
		return types.DomainEvent{}, fmt.Errorf("flowstate: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstate: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return types.DomainEvent{}, fmt.Errorf("flowstate: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return types.DomainEvent{}, fmt.Errorf("flowstate: commit tx: %w", err)
	}

	return event, nil
}

// runPostCommit executes all post-commit side effects (event bus, activities, tasks, children)
// and builds the TransitionResult. Failures are collected as warnings — the transition has
// already been committed and is considered successful.
func (e *Engine) runPostCommit(
	ctx context.Context,
	def *types.Definition,
	instance *types.WorkflowInstance,
	tr types.TransitionDef,
	event types.DomainEvent,
	previousState string,
	params map[string]any,
	start time.Time,
) (*types.TransitionResult, error) {
	now := event.CreatedAt
	targetState := instance.CurrentState
	var warnings []types.PostCommitWarning

	// 1. Emit event to bus
	if e.deps.EventBus != nil {
		if emitErr := e.deps.EventBus.Emit(ctx, event); emitErr != nil {
			warnings = append(warnings, types.PostCommitWarning{Operation: "EventBus.Emit", Err: emitErr})
			e.deps.Hooks.OnPostCommitError(ctx, "EventBus.Emit", emitErr)
		}
	}

	// 2. Dispatch activities
	var activitiesDispatched []string
	if len(tr.Activities) > 0 && e.deps.ActivityRunner != nil {
		for _, actDef := range tr.Activities {
			invocation := types.ActivityInvocation{
				ID:            generateID(),
				ActivityName:  actDef.Name,
				WorkflowType:  def.WorkflowType,
				AggregateType: instance.AggregateType,
				AggregateID:   instance.AggregateID,
				CorrelationID: instance.CorrelationID,
				Mode:          actDef.Mode,
				Input: types.ActivityInput{
					WorkflowType:  def.WorkflowType,
					AggregateType: instance.AggregateType,
					AggregateID:   instance.AggregateID,
					CorrelationID: instance.CorrelationID,
					Params:        params,
					ScheduledAt:   now,
				},
				RetryPolicy: actDef.RetryPolicy,
				Timeout:     actDef.Timeout,
				Status:      types.ActivityStatusScheduled,
				MaxAttempts: 1,
				ScheduledAt: now,
			}
			if actDef.RetryPolicy != nil {
				invocation.MaxAttempts = actDef.RetryPolicy.MaxAttempts
			}
			if e.deps.ActivityStore != nil {
				if createErr := e.deps.ActivityStore.Create(ctx, nil, invocation); createErr != nil {
					warnings = append(warnings, types.PostCommitWarning{Operation: "ActivityStore.Create", Err: createErr})
					e.deps.Hooks.OnPostCommitError(ctx, "ActivityStore.Create", createErr)
				}
			}
			if dispatchErr := e.deps.ActivityRunner.Dispatch(ctx, invocation); dispatchErr != nil {
				warnings = append(warnings, types.PostCommitWarning{Operation: "ActivityRunner.Dispatch", Err: dispatchErr})
				e.deps.Hooks.OnPostCommitError(ctx, "ActivityRunner.Dispatch", dispatchErr)
			} else {
				activitiesDispatched = append(activitiesDispatched, actDef.Name)
				e.deps.Hooks.OnActivityDispatched(ctx, invocation)
			}
		}
	}

	// 3. Create pending task
	var taskCreated *types.PendingTask
	if tr.TaskDef != nil && e.deps.TaskStore != nil {
		task := types.PendingTask{
			ID:            generateID(),
			WorkflowType:  def.WorkflowType,
			AggregateType: instance.AggregateType,
			AggregateID:   instance.AggregateID,
			CorrelationID: instance.CorrelationID,
			TaskType:      tr.TaskDef.Type,
			Description:   tr.TaskDef.Description,
			Options:       tr.TaskDef.Options,
			Status:        types.TaskStatusPending,
			Timeout:       tr.TaskDef.Timeout,
			CreatedAt:     now,
		}
		if tr.TaskDef.Timeout > 0 {
			task.ExpiresAt = now.Add(tr.TaskDef.Timeout)
		}
		if createErr := e.deps.TaskStore.Create(ctx, nil, task); createErr != nil {
			warnings = append(warnings, types.PostCommitWarning{Operation: "TaskStore.Create", Err: createErr})
			e.deps.Hooks.OnPostCommitError(ctx, "TaskStore.Create", createErr)
		} else {
			taskCreated = &task
		}
	}

	// 4. Spawn child workflow(s)
	var childrenSpawned []types.ChildRelation
	if tr.ChildDef != nil && e.deps.ChildStore != nil {
		childAggID := generateID()
		relation := types.ChildRelation{
			ID:                  generateID(),
			ParentWorkflowType:  def.WorkflowType,
			ParentAggregateType: instance.AggregateType,
			ParentAggregateID:   instance.AggregateID,
			ChildWorkflowType:   tr.ChildDef.WorkflowType,
			ChildAggregateType:  tr.ChildDef.WorkflowType,
			ChildAggregateID:    childAggID,
			CorrelationID:       instance.CorrelationID,
			Status:              types.ChildStatusActive,
			CreatedAt:           now,
		}
		if createErr := e.deps.ChildStore.Create(ctx, nil, relation); createErr != nil {
			warnings = append(warnings, types.PostCommitWarning{Operation: "ChildStore.Create", Err: createErr})
			e.deps.Hooks.OnPostCommitError(ctx, "ChildStore.Create", createErr)
		} else {
			childrenSpawned = append(childrenSpawned, relation)
		}
	}
	if tr.ChildrenDef != nil && e.deps.ChildStore != nil {
		groupID := generateID()
		inputs := tr.ChildrenDef.InputsFn(nil)
		for range inputs {
			childAggID := generateID()
			relation := types.ChildRelation{
				ID:                  generateID(),
				GroupID:             groupID,
				ParentWorkflowType:  def.WorkflowType,
				ParentAggregateType: instance.AggregateType,
				ParentAggregateID:   instance.AggregateID,
				ChildWorkflowType:   tr.ChildrenDef.WorkflowType,
				ChildAggregateType:  tr.ChildrenDef.WorkflowType,
				ChildAggregateID:    childAggID,
				CorrelationID:       instance.CorrelationID,
				JoinPolicy:          tr.ChildrenDef.Join.Mode,
				Status:              types.ChildStatusActive,
				CreatedAt:           now,
			}
			if createErr := e.deps.ChildStore.Create(ctx, nil, relation); createErr != nil {
				warnings = append(warnings, types.PostCommitWarning{Operation: "ChildStore.Create", Err: createErr})
				e.deps.Hooks.OnPostCommitError(ctx, "ChildStore.Create", createErr)
			} else {
				childrenSpawned = append(childrenSpawned, relation)
			}
		}
	}

	// 5. Build result
	isTerminal := false
	if st, exists := def.States[targetState]; exists && st.IsTerminal {
		isTerminal = true
	}
	result := &types.TransitionResult{
		Instance:             *instance,
		Event:                event,
		PreviousState:        previousState,
		NewState:             targetState,
		TransitionName:       event.TransitionName,
		ActivitiesDispatched: activitiesDispatched,
		TaskCreated:          taskCreated,
		ChildrenSpawned:      childrenSpawned,
		IsTerminal:           isTerminal,
		Warnings:             warnings,
	}

	// 6. Hook
	duration := e.deps.Clock.Now().Sub(start)
	e.deps.Hooks.OnTransition(ctx, *result, duration)

	return result, nil
}

// Signal sends a signal to trigger a matching OnSignal transition.
func (e *Engine) Signal(ctx context.Context, input types.SignalInput) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	// Load or create instance, then resolve definition version
	instance, err := e.deps.InstanceStore.Get(ctx, input.TargetAggregateType, input.TargetAggregateID)
	var def *types.Definition
	if err != nil {
		if errors.Is(err, e.deps.ErrInstanceNotFound) {
			latestDef, ok := e.definitionFor(input.TargetAggregateType, 0)
			if !ok {
				return nil, fmt.Errorf("flowstate: no workflow registered for aggregate type %q", input.TargetAggregateType)
			}
			instance, err = e.createInstance(ctx, latestDef, input.TargetAggregateType, input.TargetAggregateID)
			if err != nil {
				return nil, err
			}
			def = latestDef
		} else {
			return nil, fmt.Errorf("flowstate: get instance: %w", err)
		}
	} else {
		var ok bool
		def, ok = e.definitionFor(input.TargetAggregateType, instance.WorkflowVersion)
		if !ok {
			return nil, fmt.Errorf("flowstate: no workflow version %d registered for aggregate type %q",
				instance.WorkflowVersion, input.TargetAggregateType)
		}
	}

	// Find matching signal transition from current state
	var matches []string
	for name, tr := range def.Transitions {
		if tr.TriggerType == types.TriggerSignal && tr.TriggerKey == input.SignalName {
			if slices.Contains(tr.Sources, instance.CurrentState) {
				matches = append(matches, name)
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("flowstate: no transition matches signal %q from state %q: %w",
			input.SignalName, instance.CurrentState, e.deps.ErrNoMatchingSignal)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("flowstate: multiple transitions match signal %q from state %q: %w",
			input.SignalName, instance.CurrentState, e.deps.ErrSignalAmbiguous)
	}

	// Execute the matched transition
	return e.Transition(ctx, input.TargetAggregateType, input.TargetAggregateID, matches[0], input.ActorID, input.Payload)
}

// CompleteTask completes a pending task and fires the matching OnTaskCompleted transition.
// The choice determines which transition fires when multiple OnTaskCompleted transitions exist.
func (e *Engine) CompleteTask(ctx context.Context, taskID, choice, actorID string) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()
	if e.deps.TaskStore == nil {
		return nil, fmt.Errorf("flowstate: TaskStore not configured")
	}

	// Get the task
	task, err := e.deps.TaskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get task: %w", err)
	}

	// Load instance
	instance, err := e.deps.InstanceStore.Get(ctx, task.AggregateType, task.AggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get instance: %w", err)
	}

	// Look up definition for instance's version
	def, ok := e.definitionFor(task.AggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstate: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, task.AggregateType)
	}

	// Find matching OnTaskCompleted transitions from current state
	var matches []string
	for name, tr := range def.Transitions {
		if tr.TriggerType == types.TriggerTaskCompleted && tr.TriggerKey == task.TaskType {
			if slices.Contains(tr.Sources, instance.CurrentState) {
				matches = append(matches, name)
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("flowstate: no transition matches task completion for type %q: %w",
			task.TaskType, e.deps.ErrNoMatchingSignal)
	}

	// If multiple matches, use choice to disambiguate
	transitionName := matches[0]
	if len(matches) > 1 {
		found := false
		for _, name := range matches {
			// Match by transition name containing the choice
			if name == choice {
				transitionName = name
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("flowstate: choice %q does not match any transition: %w",
				choice, e.deps.ErrInvalidChoice)
		}
	}

	// Complete the task in store
	if err := e.deps.TaskStore.Complete(ctx, nil, taskID, choice, actorID); err != nil {
		return nil, fmt.Errorf("flowstate: complete task in store: %w", err)
	}

	// Execute the matched transition
	return e.Transition(ctx, task.AggregateType, task.AggregateID, transitionName, actorID, map[string]any{
		"_task_id":     taskID,
		"_task_choice": choice,
	})
}

// ChildCompleted notifies the parent workflow that a child has reached a terminal state.
// For single children, it fires OnChildCompleted. For grouped children, it evaluates
// the join policy and fires OnChildrenJoined when satisfied.
func (e *Engine) ChildCompleted(ctx context.Context, childAggregateType, childAggregateID, terminalState string) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	if e.deps.ChildStore == nil {
		return nil, fmt.Errorf("flowstate: ChildStore not configured")
	}

	// Look up child relation
	relation, err := e.deps.ChildStore.GetByChild(ctx, childAggregateType, childAggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get child relation: %w", err)
	}

	// Mark child as completed
	if completeErr := e.deps.ChildStore.Complete(ctx, nil, childAggregateType, childAggregateID, terminalState); completeErr != nil {
		e.deps.Hooks.OnPostCommitError(ctx, "ChildStore.Complete", completeErr)
	}

	// Load parent instance
	instance, err := e.deps.InstanceStore.Get(ctx, relation.ParentAggregateType, relation.ParentAggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get parent instance: %w", err)
	}

	// Look up parent definition for instance's version
	def, ok := e.definitionFor(relation.ParentAggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstate: no workflow version %d registered for parent aggregate type %q",
			instance.WorkflowVersion, relation.ParentAggregateType)
	}

	// If this child belongs to a group, evaluate join policy
	if relation.GroupID != "" {
		return e.evaluateJoinPolicy(ctx, relation, def, instance)
	}

	// Single child: find matching OnChildCompleted transition
	for name, tr := range def.Transitions {
		if tr.TriggerType == types.TriggerChildCompleted && tr.TriggerKey == childAggregateType {
			if slices.Contains(tr.Sources, instance.CurrentState) {
				return e.Transition(ctx, relation.ParentAggregateType, relation.ParentAggregateID, name, "system", map[string]any{
					"_child_aggregate_type": childAggregateType,
					"_child_aggregate_id":   childAggregateID,
					"_child_terminal_state": terminalState,
				})
			}
		}
	}

	return nil, fmt.Errorf("flowstate: no OnChildCompleted transition matches child type %q from parent state %q: %w",
		childAggregateType, instance.CurrentState, e.deps.ErrNoMatchingSignal)
}

// evaluateJoinPolicy checks if the join policy for a group of children is satisfied.
func (e *Engine) evaluateJoinPolicy(
	ctx context.Context,
	relation *types.ChildRelation,
	def *types.Definition,
	instance *types.WorkflowInstance,
) (*types.TransitionResult, error) {
	siblings, err := e.deps.ChildStore.GetByGroup(ctx, relation.GroupID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get siblings by group: %w", err)
	}

	completedCount := 0
	total := len(siblings)
	for _, s := range siblings {
		if s.Status == types.ChildStatusCompleted {
			completedCount++
		}
	}

	joinMode := relation.JoinPolicy
	satisfied := false
	switch joinMode {
	case "ALL", "":
		satisfied = completedCount == total
	case "ANY":
		satisfied = completedCount >= 1
	case "N":
		// Find the ChildrenDef to get the count
		for _, tr := range def.Transitions {
			if tr.ChildrenDef != nil && tr.ChildrenDef.Join.Mode == "N" {
				satisfied = completedCount >= tr.ChildrenDef.Join.Count
				break
			}
		}
	}

	if !satisfied {
		return nil, fmt.Errorf("flowstate: join policy %q not yet satisfied (%d/%d completed): %w",
			joinMode, completedCount, total, e.deps.ErrNoMatchingSignal)
	}

	// Find matching OnChildrenJoined transition
	for name, tr := range def.Transitions {
		if tr.TriggerType == types.TriggerChildrenJoined {
			if slices.Contains(tr.Sources, instance.CurrentState) {
				return e.Transition(ctx, relation.ParentAggregateType, relation.ParentAggregateID, name, "system", map[string]any{
					"_group_id":        relation.GroupID,
					"_completed_count": completedCount,
					"_total_count":     total,
				})
			}
		}
	}

	return nil, fmt.Errorf("flowstate: no OnChildrenJoined transition from parent state %q: %w",
		instance.CurrentState, e.deps.ErrNoMatchingSignal)
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
		return nil, fmt.Errorf("flowstate: get instance: %w", err)
	}

	// Look up definition for instance's version
	def, ok := e.definitionFor(aggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstate: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, aggregateType)
	}

	// Validate target state exists in definition
	if _, exists := def.States[targetState]; !exists {
		return nil, fmt.Errorf("flowstate: target state %q not found in workflow %q: %w",
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
		return nil, fmt.Errorf("flowstate: begin tx: %w", err)
	}
	if err := e.deps.EventStore.Append(ctx, tx, event); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstate: append event: %w", err)
	}
	if err := e.deps.InstanceStore.Update(ctx, tx, *instance); err != nil {
		_ = e.deps.TxProvider.Rollback(ctx, tx)
		return nil, fmt.Errorf("flowstate: update instance: %w", err)
	}
	if err := e.deps.TxProvider.Commit(ctx, tx); err != nil {
		return nil, fmt.Errorf("flowstate: commit tx: %w", err)
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

func (e *Engine) resolveRoute(ctx context.Context, tr types.TransitionDef, aggregate any, params map[string]any) (string, error) {
	var defaultTarget string
	for _, route := range tr.Routes {
		if route.IsDefault {
			defaultTarget = route.Target
			continue
		}
		if route.Condition != nil {
			matched, err := route.Condition.Evaluate(ctx, aggregate, params)
			if err != nil {
				return "", fmt.Errorf("flowstate: condition evaluation failed: %w", err)
			}
			if matched {
				return route.Target, nil
			}
		}
	}
	if defaultTarget != "" {
		return defaultTarget, nil
	}
	return "", fmt.Errorf("flowstate: no condition matched and no default route: %w", e.deps.ErrNoMatchingRoute)
}

func (e *Engine) runGuards(ctx context.Context, workflowType string, tr types.TransitionDef, aggregate any, params map[string]any) error {
	for _, guard := range tr.Guards {
		if err := guard.Check(ctx, aggregate, params); err != nil {
			guardName := fmt.Sprintf("%T", guard)
			e.deps.Hooks.OnGuardFailed(ctx, workflowType, tr.Name, guardName, err)
			return &types.GuardError{GuardName: guardName, Reason: err}
		}
	}
	return nil
}


func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		// Recursively deep-copy nested maps so event state snapshots are independent.
		if nested, ok := v.(map[string]any); ok {
			cp[k] = copyMap(nested)
		} else {
			cp[k] = v
		}
	}
	return cp
}
