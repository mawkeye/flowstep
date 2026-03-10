// Package slogadapter provides a structured logging observer for flowstep engines
// using Go's standard log/slog package.
//
// Register with an engine via:
//
//	flowstep.WithObservers(slogadapter.New(logger))
package slogadapter

import (
	"context"
	"log/slog"

	"github.com/mawkeye/flowstep/types"
)

// Compile-time interface satisfaction checks.
var (
	_ types.TransitionObserver      = (*Observer)(nil)
	_ types.GuardObserver           = (*Observer)(nil)
	_ types.ActivityObserver        = (*Observer)(nil)
	_ types.InfrastructureObserver  = (*Observer)(nil)
)

// Observer implements all four flowstep observer interfaces and logs each event
// via a *slog.Logger at configurable levels.
type Observer struct {
	logger *slog.Logger

	transitionLevel         slog.Level
	guardFailedLevel        slog.Level
	activityDispatchedLevel slog.Level
	activityCompletedLevel  slog.Level
	activityFailedLevel     slog.Level
	stuckLevel              slog.Level
	postCommitErrorLevel    slog.Level
}

// New creates an Observer that logs to logger. Functional options override the
// default log levels.
func New(logger *slog.Logger, opts ...Option) *Observer {
	o := &Observer{
		logger:                  logger,
		transitionLevel:         slog.LevelInfo,
		guardFailedLevel:        slog.LevelWarn,
		activityDispatchedLevel: slog.LevelInfo,
		activityCompletedLevel:  slog.LevelInfo,
		activityFailedLevel:     slog.LevelError,
		stuckLevel:              slog.LevelError,
		postCommitErrorLevel:    slog.LevelError,
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OnTransition implements types.TransitionObserver.
func (o *Observer) OnTransition(ctx context.Context, event types.TransitionEvent) {
	o.logger.Log(ctx, o.transitionLevel, "flowstep.transition",
		"workflow_type", event.Result.Instance.WorkflowType,
		"aggregate_id", event.Result.Instance.AggregateID,
		"transition_name", event.Result.TransitionName,
		"duration_ms", event.Duration.Milliseconds(),
	)
}

// OnGuardFailed implements types.GuardObserver.
func (o *Observer) OnGuardFailed(ctx context.Context, event types.GuardFailureEvent) {
	o.logger.Log(ctx, o.guardFailedLevel, "flowstep.guard_failed",
		"workflow_type", event.WorkflowType,
		"transition_name", event.TransitionName,
		"guard_name", event.GuardName,
		"error", event.Err,
	)
}

// OnActivityDispatched implements types.ActivityObserver.
func (o *Observer) OnActivityDispatched(ctx context.Context, event types.ActivityDispatchedEvent) {
	o.logger.Log(ctx, o.activityDispatchedLevel, "flowstep.activity_dispatched",
		"activity_name", event.Invocation.ActivityName,
		"invocation_id", event.Invocation.ID,
		"workflow_type", event.Invocation.WorkflowType,
		"aggregate_id", event.Invocation.AggregateID,
	)
}

// OnActivityCompleted implements types.ActivityObserver.
func (o *Observer) OnActivityCompleted(ctx context.Context, event types.ActivityCompletedEvent) {
	o.logger.Log(ctx, o.activityCompletedLevel, "flowstep.activity_completed",
		"activity_name", event.Invocation.ActivityName,
		"invocation_id", event.Invocation.ID,
		"workflow_type", event.Invocation.WorkflowType,
		"aggregate_id", event.Invocation.AggregateID,
	)
}

// OnActivityFailed implements types.ActivityObserver.
func (o *Observer) OnActivityFailed(ctx context.Context, event types.ActivityFailedEvent) {
	o.logger.Log(ctx, o.activityFailedLevel, "flowstep.activity_failed",
		"activity_name", event.Invocation.ActivityName,
		"invocation_id", event.Invocation.ID,
		"workflow_type", event.Invocation.WorkflowType,
		"aggregate_id", event.Invocation.AggregateID,
		"error", event.Err,
	)
}

// OnStuck implements types.InfrastructureObserver.
func (o *Observer) OnStuck(ctx context.Context, event types.StuckEvent) {
	o.logger.Log(ctx, o.stuckLevel, "flowstep.stuck",
		"workflow_type", event.Instance.WorkflowType,
		"aggregate_id", event.Instance.AggregateID,
		"reason", event.Reason,
	)
}

// OnPostCommitError implements types.InfrastructureObserver.
func (o *Observer) OnPostCommitError(ctx context.Context, event types.PostCommitErrorEvent) {
	o.logger.Log(ctx, o.postCommitErrorLevel, "flowstep.post_commit_error",
		"operation", event.Operation,
		"error", event.Err,
	)
}
