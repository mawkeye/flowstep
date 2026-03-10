// Package goroutinerunner provides an ActivityRunner that dispatches activities
// as goroutines. Suitable for simple use cases where a full task queue is not needed.
package goroutinerunner

import (
	"context"
	"fmt"
	"sync"

	"github.com/mawkeye/flowstep"
	"github.com/mawkeye/flowstep/types"
)

// Runner dispatches activities as goroutines and collects results.
type Runner struct {
	mu         sync.RWMutex
	activities map[string]flowstep.Activity
	wg         sync.WaitGroup

	errMu  sync.Mutex
	errors []error
}

// New creates a new goroutine-based ActivityRunner.
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

// Dispatch launches the activity in a new goroutine.
func (r *Runner) Dispatch(ctx context.Context, invocation types.ActivityInvocation) error {
	r.mu.RLock()
	activity, ok := r.activities[invocation.ActivityName]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("goroutinerunner: activity %q not registered", invocation.ActivityName)
	}

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()

		actCtx := ctx
		if invocation.Timeout > 0 {
			var cancel context.CancelFunc
			actCtx, cancel = context.WithTimeout(ctx, invocation.Timeout)
			defer cancel()
		}

		_, err := activity.Execute(actCtx, invocation.Input)
		if err != nil {
			r.errMu.Lock()
			r.errors = append(r.errors, fmt.Errorf("activity %s: %w", invocation.ActivityName, err))
			r.errMu.Unlock()
		}
	}()

	return nil
}

// Wait blocks until all dispatched activities complete.
func (r *Runner) Wait() {
	r.wg.Wait()
}

// Errors returns any errors collected from dispatched activities.
func (r *Runner) Errors() []error {
	r.errMu.Lock()
	defer r.errMu.Unlock()
	errs := make([]error, len(r.errors))
	copy(errs, r.errors)
	return errs
}
