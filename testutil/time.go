package testutil

import (
	"sync"
	"time"
)

// FakeClock is a controllable clock for testing time-dependent behavior.
type FakeClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewFakeClock creates a FakeClock set to a fixed starting time.
func NewFakeClock() *FakeClock {
	return &FakeClock{
		now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by the given duration.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Set sets the clock to a specific time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
