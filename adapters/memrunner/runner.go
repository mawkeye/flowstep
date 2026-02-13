package memrunner

import (
	"context"
	"sync"

	"github.com/mawkeye/flowstate"
	"github.com/mawkeye/flowstate/types"
)

// Runner is a synchronous in-memory ActivityRunner for testing.
// It calls Activity.Execute inline when Dispatch is called.
type Runner struct {
	mu         sync.RWMutex
	activities map[string]flowstate.Activity
}

// New creates a new synchronous ActivityRunner.
func New() *Runner {
	return &Runner{
		activities: make(map[string]flowstate.Activity),
	}
}

// Register adds an activity implementation.
func (r *Runner) Register(activity flowstate.Activity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activities[activity.Name()] = activity
}

// Dispatch executes the activity synchronously.
func (r *Runner) Dispatch(ctx context.Context, invocation types.ActivityInvocation) error {
	r.mu.RLock()
	activity, ok := r.activities[invocation.ActivityName]
	r.mu.RUnlock()

	if !ok {
		return flowstate.ErrActivityNotRegistered
	}

	_, err := activity.Execute(ctx, invocation.Input)
	return err
}
