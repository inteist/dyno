package benchtest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"dyno/internal/bench"
	"dyno/internal/bench/benchtest"
	"dyno/internal/domain"
)

var (
	_ bench.Clock    = (*benchtest.Clock)(nil)
	_ bench.Recorder = (*benchtest.Recorder)(nil)
)

func TestClock(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	var seen []time.Duration
	c := benchtest.NewClock(start).OnSleep(func(d time.Duration) { seen = append(seen, d) })
	c.Advance(time.Second)
	if err := c.Sleep(context.Background(), time.Minute); err != nil {
		t.Fatal(err)
	}
	if got := c.Now().Sub(start); got != time.Minute+time.Second {
		t.Errorf("elapsed = %s", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("Sleep with a canceled context = %v", err)
	}
	if !slices.Equal(c.Sleeps(), []time.Duration{time.Minute}) || len(seen) != 2 {
		t.Errorf("Sleeps() = %v, seen = %v", c.Sleeps(), seen)
	}
}

func TestRecorderModelStorageKeys(t *testing.T) {
	t.Parallel()
	r := benchtest.NewRecorder()
	models := []domain.Model{
		{Name: "a", Identity: "session:s1:a"},
		{Name: "b", Identity: "session:s1:b"},
		{Name: "a", Identity: "session:s2:a"},
		{Name: "a", Identity: "openai:a:r1", IdentitySource: "operator-revision", Revision: "r1"},
		{Name: "native", Digest: "d"},
	}
	for range 2 {
		for _, m := range models {
			if err := r.SaveModel(context.Background(), m); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := r.Models(); !slices.Equal(got, models) {
		t.Errorf("models = %v, want distinct full identities in insertion order", got)
	}
}

func TestRecorder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := benchtest.NewRecorder()
	s := domain.Session{ID: "s1"}
	if err := r.StartSession(ctx, s); err != nil {
		t.Fatal(err)
	}
	if err := r.StartSession(ctx, s); err == nil {
		t.Error("a second StartSession with the same ID gave no error")
	}
	m := domain.Model{Name: "a", Digest: "d"}
	_ = r.SaveModel(ctx, m)
	_ = r.SaveModel(ctx, m)
	for _, kind := range []domain.ExchangeKind{domain.KindWarmup, domain.KindRun} {
		if err := r.SaveExchange(ctx, domain.Exchange{Session: "s1", Kind: kind}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.SaveExchange(ctx, domain.Exchange{Session: "s2"}); err == nil {
		t.Error("SaveExchange for an unknown session gave no error")
	}
	at := time.Unix(100, 0)
	if err := r.FinishSession(ctx, "s1", domain.StatusCompleted, at); err != nil {
		t.Fatal(err)
	}
	if err := r.FinishSession(ctx, "s2", domain.StatusCompleted, at); err == nil {
		t.Error("FinishSession for an unknown session gave no error")
	}
	rec, ok := r.Session("s1")
	if !ok || rec.Status != domain.StatusCompleted || !rec.FinishedAt.Equal(at) {
		t.Errorf("Session() = %+v, %v", rec, ok)
	}
	if len(r.Models()) != 1 || len(r.Exchanges()) != 2 || len(r.ExchangesOf(domain.KindRun)) != 1 {
		t.Errorf("models = %v, exchanges = %v", r.Models(), r.Exchanges())
	}

	boom := errors.New("disk full")
	r.FailSaveExchange(boom)
	if err := r.SaveExchange(ctx, domain.Exchange{Session: "s1"}); !errors.Is(err, boom) {
		t.Errorf("SaveExchange after FailSaveExchange = %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for name, err := range map[string]error{
		"StartSession":  r.StartSession(canceled, domain.Session{ID: "s3"}),
		"SaveModel":     r.SaveModel(canceled, m),
		"SaveExchange":  r.SaveExchange(canceled, domain.Exchange{Session: "s1"}),
		"FinishSession": r.FinishSession(canceled, "s1", domain.StatusAborted, at),
	} {
		if !errors.Is(err, context.Canceled) {
			t.Errorf("%s with a canceled context = %v", name, err)
		}
	}
}
