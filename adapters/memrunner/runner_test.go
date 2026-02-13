package memrunner

import (
	"context"
	"errors"
	"testing"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

type testActivity struct {
	name   string
	called bool
}

func (a *testActivity) Name() string { return a.name }
func (a *testActivity) Execute(_ context.Context, _ types.ActivityInput) (*types.ActivityResult, error) {
	a.called = true
	return &types.ActivityResult{Output: map[string]any{"ok": true}}, nil
}

func TestRunnerDispatch(t *testing.T) {
	runner := New()
	act := &testActivity{name: "send_email"}
	runner.Register(act)

	ctx := context.Background()
	inv := types.ActivityInvocation{
		ID:           "inv-1",
		ActivityName: "send_email",
	}

	if err := runner.Dispatch(ctx, inv); err != nil {
		t.Fatalf("dispatch failed: %v", err)
	}
	if !act.called {
		t.Error("expected activity to be called")
	}
}

func TestRunnerDispatchUnregistered(t *testing.T) {
	runner := New()
	ctx := context.Background()
	inv := types.ActivityInvocation{
		ID:           "inv-1",
		ActivityName: "unknown",
	}

	err := runner.Dispatch(ctx, inv)
	if !errors.Is(err, flowstate.ErrActivityNotRegistered) {
		t.Errorf("expected ErrActivityNotRegistered, got %v", err)
	}
}
