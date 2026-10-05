package bench

import (
	"context"
	"time"
)

// Clock gives the time and waits. Tests use benchtest.Clock.
type Clock interface {
	Now() time.Time
	// Sleep waits for d. It returns ctx.Err() if ctx ends first.
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock is the real clock.
type SystemClock struct{}

// Now returns time.Now().
func (SystemClock) Now() time.Time { return time.Now() }

// Sleep waits for d or until ctx ends.
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
