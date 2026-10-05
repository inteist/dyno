package bench

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSystemClockSleep(t *testing.T) {
	t.Parallel()
	var c SystemClock
	start := c.Now()
	if err := c.Sleep(context.Background(), 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 5*time.Millisecond {
		t.Error("Sleep returned too early")
	}
	if err := c.Sleep(context.Background(), 0); err != nil {
		t.Errorf("Sleep(0) = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("Sleep with a canceled context = %v", err)
	}
	if err := c.Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("Sleep(0) with a canceled context = %v", err)
	}
}
