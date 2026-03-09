package flowstate

import (
	"time"

	"github.com/mawkeye/flowstate/types"
)

// Clock provides deterministic time for the engine.
type Clock = types.Clock

// RealClock uses time.Now().
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }
