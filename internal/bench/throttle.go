package bench

import "time"

// throttle lets the first call through, then at most one call for each
// interval of the clock.
type throttle struct {
	clock  Clock
	every  time.Duration
	passed bool      // a call went through
	next   time.Time // the earliest time of the next call
}

// newThrottle returns a throttle that lets one call through for each every.
func newThrottle(clock Clock, every time.Duration) *throttle {
	return &throttle{clock: clock, every: every}
}

// allow reports whether a call may go through now.
func (t *throttle) allow() bool {
	now := t.clock.Now()
	if t.passed && now.Before(t.next) {
		return false
	}
	t.passed, t.next = true, now.Add(t.every)
	return true
}
