package flowstate

import "time"

// Clock provides deterministic time for the engine.
type Clock interface {
	Now() time.Time
}

// RealClock uses time.Now().
type RealClock struct{}

// Now returns the current time.
func (RealClock) Now() time.Time { return time.Now() }
