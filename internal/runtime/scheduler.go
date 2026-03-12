package runtime

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mawkeye/flowstep/internal/graph"
	"github.com/mawkeye/flowstep/types"
)

// Signal sends a signal to trigger a matching OnSignal transition.
func (e *Engine) Signal(ctx context.Context, input types.SignalInput) (*types.TransitionResult, error) {
	if err := e.checkShutdown(); err != nil {
		return nil, err
	}
	e.wg.Add(1)
	defer e.wg.Done()

	// Load or create instance, then resolve compiled machine version
	instance, err := e.deps.InstanceStore.Get(ctx, input.TargetAggregateType, input.TargetAggregateID)
	var cm *graph.CompiledMachine
	if err != nil {
		if errors.Is(err, e.deps.ErrInstanceNotFound) {
			latestCm, ok := e.compiledFor(input.TargetAggregateType, 0)
			if !ok {
				return nil, fmt.Errorf("flowstep: no workflow registered for aggregate type %q", input.TargetAggregateType)
			}
			instance, err = e.createInstance(ctx, latestCm, input.TargetAggregateType, input.TargetAggregateID)
			if err != nil {
				return nil, err
			}
			cm = latestCm
		} else {
			return nil, fmt.Errorf("flowstep: get instance: %w", err)
		}
	} else {
		var ok bool
		cm, ok = e.compiledFor(input.TargetAggregateType, instance.WorkflowVersion)
		if !ok {
			return nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
				instance.WorkflowVersion, input.TargetAggregateType)
		}
	}

	// Find matching signal transition from current state (or any active parallel leaf).
	active := activeStates(instance)
	var matches []string
	for name, tr := range cm.Definition.Transitions {
		if tr.TriggerType == types.TriggerSignal && tr.TriggerKey == input.SignalName {
			for _, s := range active {
				if slices.Contains(tr.Sources, s) {
					matches = append(matches, name)
					break
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("flowstep: no transition matches signal %q from state %q: %w",
			input.SignalName, instance.CurrentState, e.deps.ErrNoMatchingSignal)
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("flowstep: multiple transitions match signal %q from state %q: %w",
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
		return nil, fmt.Errorf("flowstep: TaskStore not configured")
	}

	// Get the task
	task, err := e.deps.TaskStore.Get(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get task: %w", err)
	}

	// Load instance
	instance, err := e.deps.InstanceStore.Get(ctx, task.AggregateType, task.AggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get instance: %w", err)
	}

	// Look up compiled machine for instance's version
	cm, ok := e.compiledFor(task.AggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstep: no workflow version %d registered for aggregate type %q",
			instance.WorkflowVersion, task.AggregateType)
	}

	// Find matching OnTaskCompleted transitions from current state (or any active parallel leaf).
	active := activeStates(instance)
	var matches []string
	for name, tr := range cm.Definition.Transitions {
		if tr.TriggerType == types.TriggerTaskCompleted && tr.TriggerKey == task.TaskType {
			for _, s := range active {
				if slices.Contains(tr.Sources, s) {
					matches = append(matches, name)
					break
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("flowstep: no transition matches task completion for type %q: %w",
			task.TaskType, e.deps.ErrNoMatchingSignal)
	}

	// If multiple matches, use choice to disambiguate
	transitionName := matches[0]
	if len(matches) > 1 {
		found := false
		for _, name := range matches {
			if name == choice {
				transitionName = name
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("flowstep: choice %q does not match any transition: %w",
				choice, e.deps.ErrInvalidChoice)
		}
	}

	// Complete the task in store
	if err := e.deps.TaskStore.Complete(ctx, nil, taskID, choice, actorID); err != nil {
		return nil, fmt.Errorf("flowstep: complete task in store: %w", err)
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
		return nil, fmt.Errorf("flowstep: ChildStore not configured")
	}

	// Look up child relation
	relation, err := e.deps.ChildStore.GetByChild(ctx, childAggregateType, childAggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get child relation: %w", err)
	}

	// Mark child as completed
	if completeErr := e.deps.ChildStore.Complete(ctx, nil, childAggregateType, childAggregateID, terminalState); completeErr != nil {
		e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "ChildStore.Complete", Err: completeErr})
	}

	// Load parent instance
	instance, err := e.deps.InstanceStore.Get(ctx, relation.ParentAggregateType, relation.ParentAggregateID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get parent instance: %w", err)
	}

	// Look up compiled machine for parent instance's version
	cm, ok := e.compiledFor(relation.ParentAggregateType, instance.WorkflowVersion)
	if !ok {
		return nil, fmt.Errorf("flowstep: no workflow version %d registered for parent aggregate type %q",
			instance.WorkflowVersion, relation.ParentAggregateType)
	}

	// If this child belongs to a group, evaluate join policy
	if relation.GroupID != "" {
		return e.evaluateJoinPolicy(ctx, relation, cm, instance)
	}

	// Single child: find matching OnChildCompleted transition (or any active parallel leaf).
	active := activeStates(instance)
	for name, tr := range cm.Definition.Transitions {
		if tr.TriggerType == types.TriggerChildCompleted && tr.TriggerKey == childAggregateType {
			for _, s := range active {
				if slices.Contains(tr.Sources, s) {
					return e.Transition(ctx, relation.ParentAggregateType, relation.ParentAggregateID, name, "system", map[string]any{
						"_child_aggregate_type": childAggregateType,
						"_child_aggregate_id":   childAggregateID,
						"_child_terminal_state": terminalState,
					})
				}
			}
		}
	}

	return nil, fmt.Errorf("flowstep: no OnChildCompleted transition matches child type %q from parent state %q: %w",
		childAggregateType, instance.CurrentState, e.deps.ErrNoMatchingSignal)
}

// evaluateJoinPolicy checks if the join policy for a group of children is satisfied.
func (e *Engine) evaluateJoinPolicy(
	ctx context.Context,
	relation *types.ChildRelation,
	cm *graph.CompiledMachine,
	instance *types.WorkflowInstance,
) (*types.TransitionResult, error) {
	siblings, err := e.deps.ChildStore.GetByGroup(ctx, relation.GroupID)
	if err != nil {
		return nil, fmt.Errorf("flowstep: get siblings by group: %w", err)
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
		for _, tr := range cm.Definition.Transitions {
			if tr.ChildrenDef != nil && tr.ChildrenDef.Join.Mode == "N" {
				satisfied = completedCount >= tr.ChildrenDef.Join.Count
				break
			}
		}
	}

	if !satisfied {
		return nil, fmt.Errorf("flowstep: join policy %q not yet satisfied (%d/%d completed): %w",
			joinMode, completedCount, total, e.deps.ErrNoMatchingSignal)
	}

	// Find matching OnChildrenJoined transition (or any active parallel leaf).
	active := activeStates(instance)
	for name, tr := range cm.Definition.Transitions {
		if tr.TriggerType == types.TriggerChildrenJoined {
			for _, s := range active {
				if slices.Contains(tr.Sources, s) {
					return e.Transition(ctx, relation.ParentAggregateType, relation.ParentAggregateID, name, "system", map[string]any{
						"_group_id":        relation.GroupID,
						"_completed_count": completedCount,
						"_total_count":     total,
					})
				}
			}
		}
	}

	return nil, fmt.Errorf("flowstep: no OnChildrenJoined transition from parent state %q: %w",
		instance.CurrentState, e.deps.ErrNoMatchingSignal)
}
