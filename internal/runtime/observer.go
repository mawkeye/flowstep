package runtime

import (
	"context"

	"github.com/mawkeye/flowstep/types"
)

// ObserverRegistry holds categorized observer lists, resolved at construction time.
// Type assertions happen once in NewObserverRegistry, not per-dispatch.
// All Notify methods are safe to call on a nil *ObserverRegistry.
type ObserverRegistry struct {
	transition     []types.TransitionObserver
	guard          []types.GuardObserver
	activity       []types.ActivityObserver
	infrastructure []types.InfrastructureObserver
}

// NewObserverRegistry categorizes the given observers by type assertion.
// An observer implementing multiple interfaces is registered in all matching categories.
func NewObserverRegistry(observers []types.Observer) *ObserverRegistry {
	r := &ObserverRegistry{}
	for _, o := range observers {
		if t, ok := o.(types.TransitionObserver); ok {
			r.transition = append(r.transition, t)
		}
		if g, ok := o.(types.GuardObserver); ok {
			r.guard = append(r.guard, g)
		}
		if a, ok := o.(types.ActivityObserver); ok {
			r.activity = append(r.activity, a)
		}
		if i, ok := o.(types.InfrastructureObserver); ok {
			r.infrastructure = append(r.infrastructure, i)
		}
	}
	return r
}

// NotifyTransition delivers a TransitionEvent to all registered TransitionObservers.
func (r *ObserverRegistry) NotifyTransition(ctx context.Context, event types.TransitionEvent) {
	if r == nil {
		return
	}
	for _, o := range r.transition {
		o.OnTransition(ctx, event)
	}
}

// NotifyGuardFailed delivers a GuardFailureEvent to all registered GuardObservers.
func (r *ObserverRegistry) NotifyGuardFailed(ctx context.Context, event types.GuardFailureEvent) {
	if r == nil {
		return
	}
	for _, o := range r.guard {
		o.OnGuardFailed(ctx, event)
	}
}

// NotifyActivityDispatched delivers an ActivityDispatchedEvent to all registered ActivityObservers.
func (r *ObserverRegistry) NotifyActivityDispatched(ctx context.Context, event types.ActivityDispatchedEvent) {
	if r == nil {
		return
	}
	for _, o := range r.activity {
		o.OnActivityDispatched(ctx, event)
	}
}

// NotifyActivityCompleted delivers an ActivityCompletedEvent to all registered ActivityObservers.
func (r *ObserverRegistry) NotifyActivityCompleted(ctx context.Context, event types.ActivityCompletedEvent) {
	if r == nil {
		return
	}
	for _, o := range r.activity {
		o.OnActivityCompleted(ctx, event)
	}
}

// NotifyActivityFailed delivers an ActivityFailedEvent to all registered ActivityObservers.
func (r *ObserverRegistry) NotifyActivityFailed(ctx context.Context, event types.ActivityFailedEvent) {
	if r == nil {
		return
	}
	for _, o := range r.activity {
		o.OnActivityFailed(ctx, event)
	}
}

// NotifyStuck delivers a StuckEvent to all registered InfrastructureObservers.
func (r *ObserverRegistry) NotifyStuck(ctx context.Context, event types.StuckEvent) {
	if r == nil {
		return
	}
	for _, o := range r.infrastructure {
		o.OnStuck(ctx, event)
	}
}

// NotifyPostCommitError delivers a PostCommitErrorEvent to all registered InfrastructureObservers.
func (r *ObserverRegistry) NotifyPostCommitError(ctx context.Context, event types.PostCommitErrorEvent) {
	if r == nil {
		return
	}
	for _, o := range r.infrastructure {
		o.OnPostCommitError(ctx, event)
	}
}
