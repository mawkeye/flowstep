package memrunner

import (
	"context"
	"sync"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// Runner is a synchronous in-memory ActivityRunner for testing.
// It calls Activity.Execute inline when Dispatch is called.
type Runner struct {
	mu         sync.RWMutex
	activities map[string]flowstep.Activity
}

// New creates a new synchronous ActivityRunner.
func New() *Runner {
	return &Runner{
		activities: make(map[string]flowstep.Activity),
	}
}

// Register adds an activity implementation.
func (r *Runner) Register(activity flowstep.Activity) {
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
		return flowstep.ErrActivityNotRegistered
	}

	_, err := activity.Execute(ctx, invocation.Input)
	return err
}
