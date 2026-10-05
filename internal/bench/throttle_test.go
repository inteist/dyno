package bench

import (
	"slices"
	"testing"
	"time"

	"token-rate/internal/bench/benchtest"
)

func TestThrottle(t *testing.T) {
	t.Parallel()
	clock := benchtest.NewClock(testStart())
	limit := newThrottle(clock, 100*time.Millisecond)
	steps := []time.Duration{0, 0, 50 * time.Millisecond, 49 * time.Millisecond, time.Millisecond, 0, 250 * time.Millisecond}
	var got []bool
	for _, d := range steps {
		clock.Advance(d)
		got = append(got, limit.allow())
	}
	// Times: 0 ms, 0, 50, 99, 100, 100, 350.
	if want := []bool{true, false, false, false, true, false, true}; !slices.Equal(got, want) {
		t.Errorf("allow() = %v, want %v", got, want)
	}
}
