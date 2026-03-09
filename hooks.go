package flowstate

import (
	"context"
	"time"

	"github.com/mawkeye/flowstate/types"
)

// Hooks allows consumers to observe engine behavior.
// All methods MUST be non-blocking.
type Hooks = types.Hooks

// NoopHooks is the default Hooks implementation. Does nothing.
type NoopHooks struct{}

func (NoopHooks) OnTransition(context.Context, types.TransitionResult, time.Duration)                {}
func (NoopHooks) OnGuardFailed(context.Context, string, string, string, error)                       {}
func (NoopHooks) OnActivityDispatched(context.Context, types.ActivityInvocation)                      {}
func (NoopHooks) OnActivityCompleted(context.Context, types.ActivityInvocation, *types.ActivityResult) {}
func (NoopHooks) OnActivityFailed(context.Context, types.ActivityInvocation, error)                   {}
func (NoopHooks) OnStuck(context.Context, types.WorkflowInstance, string)                             {}
func (NoopHooks) OnPostCommitError(context.Context, string, error)                                    {}
