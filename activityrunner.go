package flowstate

import "github.com/mawkeye/flowstate/types"

// Activity performs non-deterministic work outside the workflow transaction.
// Can contain any code: API calls, DB writes, file I/O, network requests.
// flowstate does NOT recover or replay activity state on failure.
type Activity = types.Activity

// ActivityRunner dispatches activity invocations for async execution.
type ActivityRunner = types.ActivityRunner
