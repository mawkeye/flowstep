package runtime

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

// ─── observer stubs ───────────────────────────────────────────────────────────

type captureTransitionObserver struct {
	events []types.TransitionEvent
}

func (c *captureTransitionObserver) OnTransition(_ context.Context, e types.TransitionEvent) {
	c.events = append(c.events, e)
}

type captureGuardObserver struct {
	events []types.GuardFailureEvent
}

func (c *captureGuardObserver) OnGuardFailed(_ context.Context, e types.GuardFailureEvent) {
	c.events = append(c.events, e)
}

type captureActivityObserver struct {
	dispatched  []types.ActivityDispatchedEvent
	completed   []types.ActivityCompletedEvent
	failed      []types.ActivityFailedEvent
}

func (c *captureActivityObserver) OnActivityDispatched(_ context.Context, e types.ActivityDispatchedEvent) {
	c.dispatched = append(c.dispatched, e)
}
func (c *captureActivityObserver) OnActivityCompleted(_ context.Context, e types.ActivityCompletedEvent) {
	c.completed = append(c.completed, e)
}
func (c *captureActivityObserver) OnActivityFailed(_ context.Context, e types.ActivityFailedEvent) {
	c.failed = append(c.failed, e)
}

type captureInfraObserver struct {
	stuck      []types.StuckEvent
	postCommit []types.PostCommitErrorEvent
}

func (c *captureInfraObserver) OnStuck(_ context.Context, e types.StuckEvent) {
	c.stuck = append(c.stuck, e)
}
func (c *captureInfraObserver) OnPostCommitError(_ context.Context, e types.PostCommitErrorEvent) {
	c.postCommit = append(c.postCommit, e)
}

// allCapture implements all 4 observer interfaces.
type allCapture struct {
	transitions int
	guards      int
	dispatched  int
	completed   int
	failed      int
	stuck       int
	postCommit  int
}

func (a *allCapture) OnTransition(_ context.Context, _ types.TransitionEvent)               { a.transitions++ }
func (a *allCapture) OnGuardFailed(_ context.Context, _ types.GuardFailureEvent)             { a.guards++ }
func (a *allCapture) OnActivityDispatched(_ context.Context, _ types.ActivityDispatchedEvent) { a.dispatched++ }
func (a *allCapture) OnActivityCompleted(_ context.Context, _ types.ActivityCompletedEvent)   { a.completed++ }
func (a *allCapture) OnActivityFailed(_ context.Context, _ types.ActivityFailedEvent)         { a.failed++ }
func (a *allCapture) OnStuck(_ context.Context, _ types.StuckEvent)                          { a.stuck++ }
func (a *allCapture) OnPostCommitError(_ context.Context, _ types.PostCommitErrorEvent)       { a.postCommit++ }

// ─── registry dispatch tests ──────────────────────────────────────────────────

func TestObserverRegistry_NotifyTransition_DispatchesToRegistered(t *testing.T) {
	obs := &captureTransitionObserver{}
	reg := NewObserverRegistry([]types.Observer{obs})
	reg.NotifyTransition(context.Background(), types.TransitionEvent{
		Result:   types.TransitionResult{NewState: "DONE"},
		Duration: time.Millisecond,
	})
	if len(obs.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(obs.events))
	}
	if obs.events[0].Result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", obs.events[0].Result.NewState)
	}
}

func TestObserverRegistry_NotifyGuardFailed_DispatchesToRegistered(t *testing.T) {
	obs := &captureGuardObserver{}
	reg := NewObserverRegistry([]types.Observer{obs})
	reg.NotifyGuardFailed(context.Background(), types.GuardFailureEvent{
		WorkflowType: "order",
		GuardName:    "myGuard",
	})
	if len(obs.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(obs.events))
	}
	if obs.events[0].GuardName != "myGuard" {
		t.Errorf("expected myGuard, got %s", obs.events[0].GuardName)
	}
}

func TestObserverRegistry_NotifyActivityDispatched_DispatchesToRegistered(t *testing.T) {
	obs := &captureActivityObserver{}
	reg := NewObserverRegistry([]types.Observer{obs})
	reg.NotifyActivityDispatched(context.Background(), types.ActivityDispatchedEvent{
		Invocation: types.ActivityInvocation{ActivityName: "send_email"},
	})
	if len(obs.dispatched) != 1 {
		t.Fatalf("expected 1 dispatched, got %d", len(obs.dispatched))
	}
}

func TestObserverRegistry_NotifyPostCommitError_DispatchesToRegistered(t *testing.T) {
	obs := &captureInfraObserver{}
	reg := NewObserverRegistry([]types.Observer{obs})
	reg.NotifyPostCommitError(context.Background(), types.PostCommitErrorEvent{
		Operation: "EventBus.Emit",
	})
	if len(obs.postCommit) != 1 {
		t.Fatalf("expected 1 post-commit error, got %d", len(obs.postCommit))
	}
	if obs.postCommit[0].Operation != "EventBus.Emit" {
		t.Errorf("expected EventBus.Emit, got %s", obs.postCommit[0].Operation)
	}
}

func TestObserverRegistry_MultipleObservers_AllReceiveEvent(t *testing.T) {
	obs1 := &captureTransitionObserver{}
	obs2 := &captureTransitionObserver{}
	reg := NewObserverRegistry([]types.Observer{obs1, obs2})
	reg.NotifyTransition(context.Background(), types.TransitionEvent{})
	if len(obs1.events) != 1 {
		t.Errorf("obs1 expected 1 event, got %d", len(obs1.events))
	}
	if len(obs2.events) != 1 {
		t.Errorf("obs2 expected 1 event, got %d", len(obs2.events))
	}
}

func TestObserverRegistry_AllInterfacesObserver_ReceivesAllEvents(t *testing.T) {
	obs := &allCapture{}
	reg := NewObserverRegistry([]types.Observer{obs})
	ctx := context.Background()
	reg.NotifyTransition(ctx, types.TransitionEvent{})
	reg.NotifyGuardFailed(ctx, types.GuardFailureEvent{})
	reg.NotifyActivityDispatched(ctx, types.ActivityDispatchedEvent{})
	reg.NotifyActivityCompleted(ctx, types.ActivityCompletedEvent{})
	reg.NotifyActivityFailed(ctx, types.ActivityFailedEvent{})
	reg.NotifyStuck(ctx, types.StuckEvent{})
	reg.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{})
	if obs.transitions != 1 {
		t.Errorf("transitions: expected 1, got %d", obs.transitions)
	}
	if obs.guards != 1 {
		t.Errorf("guards: expected 1, got %d", obs.guards)
	}
	if obs.dispatched != 1 {
		t.Errorf("dispatched: expected 1, got %d", obs.dispatched)
	}
	if obs.postCommit != 1 {
		t.Errorf("postCommit: expected 1, got %d", obs.postCommit)
	}
}

func TestObserverRegistry_NilRegistry_SafeNoOp(t *testing.T) {
	var reg *ObserverRegistry
	ctx := context.Background()
	// None of these should panic
	reg.NotifyTransition(ctx, types.TransitionEvent{})
	reg.NotifyGuardFailed(ctx, types.GuardFailureEvent{})
	reg.NotifyActivityDispatched(ctx, types.ActivityDispatchedEvent{})
	reg.NotifyActivityCompleted(ctx, types.ActivityCompletedEvent{})
	reg.NotifyActivityFailed(ctx, types.ActivityFailedEvent{})
	reg.NotifyStuck(ctx, types.StuckEvent{})
	reg.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{})
}

func TestObserverRegistry_EmptyRegistry_SafeNoOp(t *testing.T) {
	reg := NewObserverRegistry(nil)
	ctx := context.Background()
	reg.NotifyTransition(ctx, types.TransitionEvent{})
	reg.NotifyPostCommitError(ctx, types.PostCommitErrorEvent{})
}

func TestObserverRegistry_PartialObserver_OnlyReceivesMatchingEvents(t *testing.T) {
	// captureTransitionObserver only implements TransitionObserver
	obs := &captureTransitionObserver{}
	reg := NewObserverRegistry([]types.Observer{obs})
	ctx := context.Background()
	reg.NotifyGuardFailed(ctx, types.GuardFailureEvent{})     // should not reach obs
	reg.NotifyTransition(ctx, types.TransitionEvent{})         // should reach obs
	if len(obs.events) != 1 {
		t.Errorf("expected 1 transition event, got %d", len(obs.events))
	}
}
