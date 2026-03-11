package types_test

import (
	"context"
	"testing"
	"time"

	"github.com/mawkeye/flowstep/types"
)

// ─── interface satisfaction structs ──────────────────────────────────────────

type stubTransitionObserver struct{ called bool }

func (s *stubTransitionObserver) OnTransition(_ context.Context, _ types.TransitionEvent) {
	s.called = true
}

type stubGuardObserver struct{ called bool }

func (s *stubGuardObserver) OnGuardFailed(_ context.Context, _ types.GuardFailureEvent) {
	s.called = true
}

type stubActivityObserver struct{ called int }

func (s *stubActivityObserver) OnActivityDispatched(_ context.Context, _ types.ActivityDispatchedEvent) {
	s.called++
}
func (s *stubActivityObserver) OnActivityCompleted(_ context.Context, _ types.ActivityCompletedEvent) {
	s.called++
}
func (s *stubActivityObserver) OnActivityFailed(_ context.Context, _ types.ActivityFailedEvent) {
	s.called++
}

type stubInfrastructureObserver struct{ called int }

func (s *stubInfrastructureObserver) OnStuck(_ context.Context, _ types.StuckEvent) {
	s.called++
}
func (s *stubInfrastructureObserver) OnPostCommitError(_ context.Context, _ types.PostCommitErrorEvent) {
	s.called++
}

// ─── compile-time interface checks ───────────────────────────────────────────

var _ types.Observer = (*stubTransitionObserver)(nil)
var _ types.Observer = (*stubGuardObserver)(nil)
var _ types.Observer = (*stubActivityObserver)(nil)
var _ types.Observer = (*stubInfrastructureObserver)(nil)

var _ types.TransitionObserver = (*stubTransitionObserver)(nil)
var _ types.GuardObserver = (*stubGuardObserver)(nil)
var _ types.ActivityObserver = (*stubActivityObserver)(nil)
var _ types.InfrastructureObserver = (*stubInfrastructureObserver)(nil)

// ─── event struct field tests ─────────────────────────────────────────────────

func TestTransitionEvent_Fields(t *testing.T) {
	tr := types.TransitionResult{NewState: "DONE"}
	ev := types.TransitionEvent{
		Result:   tr,
		Duration: 42 * time.Millisecond,
	}
	if ev.Result.NewState != "DONE" {
		t.Errorf("expected DONE, got %s", ev.Result.NewState)
	}
	if ev.Duration != 42*time.Millisecond {
		t.Errorf("expected 42ms, got %v", ev.Duration)
	}
}

func TestGuardFailureEvent_Fields(t *testing.T) {
	ev := types.GuardFailureEvent{
		WorkflowType:   "order",
		TransitionName: "approve",
		GuardName:      "myGuard",
		Err:            nil,
	}
	if ev.WorkflowType != "order" {
		t.Errorf("expected order, got %s", ev.WorkflowType)
	}
	if ev.TransitionName != "approve" {
		t.Errorf("expected approve, got %s", ev.TransitionName)
	}
	if ev.GuardName != "myGuard" {
		t.Errorf("expected myGuard, got %s", ev.GuardName)
	}
}

func TestActivityDispatchedEvent_Fields(t *testing.T) {
	inv := types.ActivityInvocation{ActivityName: "send_email"}
	ev := types.ActivityDispatchedEvent{Invocation: inv}
	if ev.Invocation.ActivityName != "send_email" {
		t.Errorf("expected send_email, got %s", ev.Invocation.ActivityName)
	}
}

func TestActivityCompletedEvent_Fields(t *testing.T) {
	inv := types.ActivityInvocation{ActivityName: "send_email"}
	result := &types.ActivityResult{Output: map[string]any{"key": "val"}}
	ev := types.ActivityCompletedEvent{Invocation: inv, Result: result}
	if ev.Invocation.ActivityName != "send_email" {
		t.Errorf("expected send_email, got %s", ev.Invocation.ActivityName)
	}
	if ev.Result == nil {
		t.Error("expected non-nil result")
	}
}

func TestActivityFailedEvent_Fields(t *testing.T) {
	inv := types.ActivityInvocation{ActivityName: "send_email"}
	ev := types.ActivityFailedEvent{Invocation: inv, Err: nil}
	if ev.Invocation.ActivityName != "send_email" {
		t.Errorf("expected send_email, got %s", ev.Invocation.ActivityName)
	}
}

func TestStuckEvent_Fields(t *testing.T) {
	inst := types.WorkflowInstance{CurrentState: "STUCK_STATE"}
	ev := types.StuckEvent{Instance: inst, Reason: "timeout"}
	if ev.Instance.CurrentState != "STUCK_STATE" {
		t.Errorf("expected STUCK_STATE, got %s", ev.Instance.CurrentState)
	}
	if ev.Reason != "timeout" {
		t.Errorf("expected timeout, got %s", ev.Reason)
	}
}

func TestPostCommitErrorEvent_Fields(t *testing.T) {
	ev := types.PostCommitErrorEvent{Operation: "EventBus.Emit", Err: nil}
	if ev.Operation != "EventBus.Emit" {
		t.Errorf("expected EventBus.Emit, got %s", ev.Operation)
	}
}

// ─── multi-interface observer ─────────────────────────────────────────────────

type allObserver struct{}

func (allObserver) OnTransition(_ context.Context, _ types.TransitionEvent)               {}
func (allObserver) OnGuardFailed(_ context.Context, _ types.GuardFailureEvent)             {}
func (allObserver) OnActivityDispatched(_ context.Context, _ types.ActivityDispatchedEvent) {}
func (allObserver) OnActivityCompleted(_ context.Context, _ types.ActivityCompletedEvent)   {}
func (allObserver) OnActivityFailed(_ context.Context, _ types.ActivityFailedEvent)         {}
func (allObserver) OnStuck(_ context.Context, _ types.StuckEvent)                           {}
func (allObserver) OnPostCommitError(_ context.Context, _ types.PostCommitErrorEvent)        {}

var _ types.TransitionObserver = allObserver{}
var _ types.GuardObserver = allObserver{}
var _ types.ActivityObserver = allObserver{}
var _ types.InfrastructureObserver = allObserver{}
var _ types.Observer = allObserver{}

// ─── Task 6: SavepointProvider + ActivityResolver interface tests ─────────────

type stubSavepointProvider struct {
	savepointName   string
	rollbackToName  string
}

func (s *stubSavepointProvider) Begin(_ context.Context) (any, error)           { return nil, nil }
func (s *stubSavepointProvider) Commit(_ context.Context, _ any) error          { return nil }
func (s *stubSavepointProvider) Rollback(_ context.Context, _ any) error        { return nil }
func (s *stubSavepointProvider) Savepoint(_ context.Context, _ any, name string) error {
	s.savepointName = name
	return nil
}
func (s *stubSavepointProvider) RollbackTo(_ context.Context, _ any, name string) error {
	s.rollbackToName = name
	return nil
}

func TestSavepointProvider_InterfaceSatisfaction(t *testing.T) {
	var _ types.TxProvider = (*stubSavepointProvider)(nil)
	var _ types.SavepointProvider = (*stubSavepointProvider)(nil)
}

func TestSavepointProvider_MethodsCallable(t *testing.T) {
	s := &stubSavepointProvider{}
	if err := s.Savepoint(context.Background(), nil, "sp1"); err != nil {
		t.Errorf("Savepoint() error = %v", err)
	}
	if s.savepointName != "sp1" {
		t.Errorf("savepointName = %q, want sp1", s.savepointName)
	}
	if err := s.RollbackTo(context.Background(), nil, "sp1"); err != nil {
		t.Errorf("RollbackTo() error = %v", err)
	}
	if s.rollbackToName != "sp1" {
		t.Errorf("rollbackToName = %q, want sp1", s.rollbackToName)
	}
}

type stubActivityResolver struct {
	activities map[string]types.Activity
}

func (s *stubActivityResolver) Dispatch(_ context.Context, _ types.ActivityInvocation) error {
	return nil
}
func (s *stubActivityResolver) Resolve(name string) (types.Activity, bool) {
	a, ok := s.activities[name]
	return a, ok
}

func TestActivityResolver_InterfaceSatisfaction(t *testing.T) {
	var _ types.ActivityRunner = (*stubActivityResolver)(nil)
	var _ types.ActivityResolver = (*stubActivityResolver)(nil)
}

func TestActivityResolver_ResolveKnownName(t *testing.T) {
	act := &stubActivity{name: "my-activity"}
	resolver := &stubActivityResolver{
		activities: map[string]types.Activity{"my-activity": act},
	}
	got, ok := resolver.Resolve("my-activity")
	if !ok {
		t.Fatal("Resolve returned ok=false for known activity")
	}
	if got.Name() != "my-activity" {
		t.Errorf("activity name = %q, want my-activity", got.Name())
	}
}

func TestActivityResolver_ResolveUnknownName(t *testing.T) {
	resolver := &stubActivityResolver{activities: map[string]types.Activity{}}
	_, ok := resolver.Resolve("nonexistent")
	if ok {
		t.Error("Resolve returned ok=true for unknown activity")
	}
}

type stubActivity struct{ name string }

func (s *stubActivity) Name() string { return s.name }
func (s *stubActivity) Execute(_ context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	return &types.ActivityResult{}, nil
}
