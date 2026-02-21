package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mawkeye/flowstate/types"
)

// Deps holds all external dependencies injected by the root package.
// This avoids the internal engine importing the root package.
type Deps struct {
	EventStore     EventStore
	InstanceStore  InstanceStore
	TaskStore      TaskStore
	ChildStore     ChildStore
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
	ErrNoMatchingSignal  error
	ErrSignalAmbiguous   error
	ErrNoMatchingRoute   error
	ErrTaskNotFound      error
	ErrInvalidChoice     error
	ErrEngineShutdown    error
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

// ChildStore interface.
type ChildStore interface {
	Create(ctx context.Context, tx any, relation types.ChildRelation) error
	GetByChild(ctx context.Context, childAggregateType, childAggregateID string) (*types.ChildRelation, error)
	GetByParent(ctx context.Context, parentAggregateType, parentAggregateID string) ([]types.ChildRelation, error)
	GetByGroup(ctx context.Context, groupID string) ([]types.ChildRelation, error)
	Complete(ctx context.Context, tx any, childAggregateType, childAggregateID, terminalState string) error
}

// TaskStore interface.
type TaskStore interface {
	Create(ctx context.Context, tx any, task types.PendingTask) error
	Get(ctx context.Context, taskID string) (*types.PendingTask, error)
	GetByAggregate(ctx context.Context, aggregateType, aggregateID string) ([]types.PendingTask, error)
	Complete(ctx context.Context, tx any, taskID, choice, actorID string) error
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
	OnActivityDispatched(ctx context.Context, invocation types.ActivityInvocation)
	OnActivityCompleted(ctx context.Context, invocation types.ActivityInvocation, result *types.ActivityResult)
	OnActivityFailed(ctx context.Context, invocation types.ActivityInvocation, err error)
	OnStuck(ctx context.Context, instance types.WorkflowInstance, reason string)
}

// Engine executes workflow state transitions.
type Engine struct {
	deps     Deps
	versions map[string]map[int]*types.Definition // key: aggregateType -> version -> def
	latest   map[string]*types.Definition         // key: aggregateType -> latest def

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

// Shutdown gracefully stops the engine. Waits for in-flight operations to complete.
func (e *Engine) Shutdown(_ context.Context) error {
	e.shutdown.Store(true)
	e.wg.Wait()
	return nil
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
func (e *Engine) definitionFor(aggregateType string, version int) (*types.Definition, bool) {
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

	// 1. Load or create workflow instance, then resolve the correct definition version
	instance, err := e.deps.InstanceStore.Get(ctx, aggregateType, aggregateID)
	var def *types.Definition
	if err != nil {
		if !errors.Is(err, e.deps.ErrInstanceNotFound) {
			return nil, fmt.Errorf("flowstate: get instance: %w", err)
		}
		// New instance: use latest definition
		var ok bool
		def, ok = e.definitionFor(aggregateType, 0)
		if !ok {
			return nil, fmt.Errorf("flowstate: no workflow registered for aggregate type %q", aggregateType)
		}
		instance, err = e.createInstance(ctx, def, aggregateType, aggregateID)
		if err != nil {
			return nil, err
		}
	} else {
		// Existing instance: use its version
		var ok bool
		def, ok = e.definitionFor(aggregateType, instance.WorkflowVersion)
		if !ok {
			return nil, fmt.Errorf("flowstate: no workflow version %d registered for aggregate type %q",
				instance.WorkflowVersion, aggregateType)
		}
	}

	// 2. Look up transition
	tr, ok := def.Transitions[transitionName]
	if !ok {
		return nil, fmt.Errorf("flowstate: transition %q not found in workflow %q: %w",
			transitionName, def.WorkflowType, e.deps.ErrInvalidTransition)
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
	if err := e.runGuards(ctx, def.WorkflowType, tr, nil, params); err != nil {
		return nil, err
	}

	// 7. Determine target state (direct or routed)
	targetState := tr.Target
	if len(tr.Routes) > 0 {
		resolved, err := e.resolveRoute(ctx, tr, nil, params)
		if err != nil {
			return nil, err
		}
		targetState = resolved
	}

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
				MaxAttempts: 1,
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
			e.deps.Hooks.OnActivityDispatched(ctx, invocation)
		}
	}

	// 13. Post-commit: create pending task if EmitTask defined
	var taskCreated *types.PendingTask
	if tr.TaskDef != nil && e.deps.TaskStore != nil {
		task := types.PendingTask{
			ID:            generateID(),
			WorkflowType:  def.WorkflowType,
			AggregateType: aggregateType,
			AggregateID:   aggregateID,
			CorrelationID: instance.CorrelationID,
			TaskType:      tr.TaskDef.Type,
			Description:   tr.TaskDef.Description,
			Options:       tr.TaskDef.Options,
			Status:        types.TaskStatusPending,
			Timeout:       tr.TaskDef.Timeout,
			CreatedAt:     now,
		}
		if tr.TaskDef.Timeout > 0 {
			expiresAt := now.Add(tr.TaskDef.Timeout)
			task.ExpiresAt = expiresAt
		}
		_ = e.deps.TaskStore.Create(ctx, nil, task)
		taskCreated = &task
	}

	// 14. Post-commit: spawn child workflow(s)
	var childrenSpawned []types.ChildRelation
	if tr.ChildDef != nil && e.deps.ChildStore != nil {
		childAggID := generateID()
		relation := types.ChildRelation{
			ID:                  generateID(),
			ParentWorkflowType:  def.WorkflowType,
			ParentAggregateType: aggregateType,
			ParentAggregateID:   aggregateID,
			ChildWorkflowType:   tr.ChildDef.WorkflowType,
			ChildAggregateType:  tr.ChildDef.WorkflowType,
			ChildAggregateID:    childAggID,
			CorrelationID:       instance.CorrelationID,
			Status:              "ACTIVE",
			CreatedAt:           now,
		}
		_ = e.deps.ChildStore.Create(ctx, nil, relation)
		childrenSpawned = append(childrenSpawned, relation)
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
				ParentAggregateType: aggregateType,
				ParentAggregateID:   aggregateID,
				ChildWorkflowType:   tr.ChildrenDef.WorkflowType,
				ChildAggregateType:  tr.ChildrenDef.WorkflowType,
				ChildAggregateID:    childAggID,
				CorrelationID:       instance.CorrelationID,
				JoinPolicy:          tr.ChildrenDef.Join.Mode,
				Status:              "ACTIVE",
				CreatedAt:           now,
			}
			_ = e.deps.ChildStore.Create(ctx, nil, relation)
			childrenSpawned = append(childrenSpawned, relation)
		}
	}

	// 15. Build result
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
		TaskCreated:          taskCreated,
		ChildrenSpawned:      childrenSpawned,
		IsTerminal:           isTerminal,
	}

	// 15. Post-commit: hooks
	duration := e.deps.Clock.Now().Sub(start)
	e.deps.Hooks.OnTransition(ctx, *result, duration)

	return result, nil
}

// Signal sends a signal to trigger a matching OnSignal transition.
func (e *Engine) Signal(ctx context.Context, input types.SignalInput) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}

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
			if containsSource(tr.Sources, instance.CurrentState) {
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
			if containsSource(tr.Sources, instance.CurrentState) {
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
	if e.deps.ChildStore == nil {
		return nil, fmt.Errorf("flowstate: ChildStore not configured")
	}

	// Look up child relation
	relation, err := e.deps.ChildStore.GetByChild(ctx, childAggregateType, childAggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstate: get child relation: %w", err)
	}

	// Mark child as completed
	_ = e.deps.ChildStore.Complete(ctx, nil, childAggregateType, childAggregateID, terminalState)

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
			if containsSource(tr.Sources, instance.CurrentState) {
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
		if s.Status == "COMPLETED" {
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
			if containsSource(tr.Sources, instance.CurrentState) {
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

	// Emit event
	if e.deps.EventBus != nil {
		_ = e.deps.EventBus.Emit(ctx, event)
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
