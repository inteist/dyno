// Package benchtest gives fakes for the ports of the bench package. The
// fakes do not import bench, so the tests of bench can use them.
package benchtest

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Clock is a fake bench.Clock. The time moves only when a test calls
// Advance or when the code calls Sleep. Sleep returns at once. Clock is safe
// for concurrent use.
type Clock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  []time.Duration
	onSleep func(time.Duration)
}

// NewClock returns a clock that starts at start.
func NewClock(start time.Time) *Clock { return &Clock{now: start} }

// OnSleep makes Sleep call fn with the duration before Sleep checks the
// context. A test can cancel a context in fn.
func (c *Clock) OnSleep(fn func(time.Duration)) *Clock { c.onSleep = fn; return c }

// Now returns the fake time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the fake time forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// Sleep records d and moves the time forward by d. It returns ctx.Err() if
// ctx is done.
func (c *Clock) Sleep(ctx context.Context, d time.Duration) error {
	if c.onSleep != nil {
		c.onSleep(d)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	c.Advance(d)
	return nil
}

// Sleeps returns the durations of all finished Sleep calls in order.
func (c *Clock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.sleeps)
}
