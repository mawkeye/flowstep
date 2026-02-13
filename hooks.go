package flowstate

import (
	"context"
	"time"

	"github.com/mawkeye/flowstate/types"
)

// Hooks allows consumers to observe engine behavior.
// All methods MUST be non-blocking.
type Hooks interface {
	OnTransition(ctx context.Context, result types.TransitionResult, duration time.Duration)
	OnGuardFailed(ctx context.Context, workflowType, transitionName, guardName string, err error)
	OnActivityDispatched(ctx context.Context, invocation types.ActivityInvocation)
	OnActivityCompleted(ctx context.Context, invocation types.ActivityInvocation, result *types.ActivityResult)
	OnActivityFailed(ctx context.Context, invocation types.ActivityInvocation, err error)
	OnStuck(ctx context.Context, instance types.WorkflowInstance, reason string)
}

// NoopHooks is the default Hooks implementation. Does nothing.
type NoopHooks struct{}

func (NoopHooks) OnTransition(context.Context, types.TransitionResult, time.Duration)              {}
func (NoopHooks) OnGuardFailed(context.Context, string, string, string, error)                      {}
func (NoopHooks) OnActivityDispatched(context.Context, types.ActivityInvocation)                     {}
func (NoopHooks) OnActivityCompleted(context.Context, types.ActivityInvocation, *types.ActivityResult) {}
func (NoopHooks) OnActivityFailed(context.Context, types.ActivityInvocation, error)                  {}
func (NoopHooks) OnStuck(context.Context, types.WorkflowInstance, string)                            {}
