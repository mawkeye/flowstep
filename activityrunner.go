package flowstate

import (
	"context"

	"github.com/mawkeye/flowstate/types"
)

// Activity performs non-deterministic work outside the workflow transaction.
// Can contain any code: API calls, DB writes, file I/O, network requests.
// flowstate does NOT recover or replay activity state on failure.
type Activity interface {
	Name() string
	Execute(ctx context.Context, input types.ActivityInput) (*types.ActivityResult, error)
}

// ActivityRunner dispatches activity invocations for async execution.
type ActivityRunner interface {
	Dispatch(ctx context.Context, invocation types.ActivityInvocation) error
}
