package runtime

import (
	"context"
	"time"

	"github.com/mawkeye/flowstep/types"
)

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
			e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "EventBus.Emit", Err: emitErr})
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
					e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "ActivityStore.Create", Err: createErr})
				}
			}
			if dispatchErr := e.deps.ActivityRunner.Dispatch(ctx, invocation); dispatchErr != nil {
				warnings = append(warnings, types.PostCommitWarning{Operation: "ActivityRunner.Dispatch", Err: dispatchErr})
				e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "ActivityRunner.Dispatch", Err: dispatchErr})
			} else {
				activitiesDispatched = append(activitiesDispatched, actDef.Name)
				e.deps.Observers.NotifyActivityDispatched(ctx, types.ActivityDispatchedEvent{Invocation: invocation})
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
			e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "TaskStore.Create", Err: createErr})
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
			e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "ChildStore.Create", Err: createErr})
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
				e.deps.Observers.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{Operation: "ChildStore.Create", Err: createErr})
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
	e.deps.Observers.NotifyTransition(ctx, types.TransitionEvent{Result: *result, Duration: duration})

	return result, nil
}

