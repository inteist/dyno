package bench

import (
	"context"
	"dyno/internal/domain"
	"dyno/internal/llm"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestExternalFailureLimitWaitsOnlyForRunnableSteps(t *testing.T) {
	for _, names := range [][]string{{"bad"}, {"bad", "good"}, {"good", "bad"}} {
		for _, order := range []domain.Order{domain.OrderModel, domain.OrderRoundRobin} {
			t.Run(strings.Join(names, "-")+"-"+string(order), func(t *testing.T) {
				models := make([]domain.Model, len(names))
				for i, name := range names {
					models[i].Name = name
				}
				h, f, d := newExternalHarness(models...)
				h.opts.Runs, h.opts.Interval, h.opts.Order = 5, 2*time.Second, order
				f.chat = func(ctx context.Context, req llm.ChatRequest, p llm.ProgressFunc) (llm.ChatResult, error) {
					if req.Model.Name == "bad" && req.Options.MaxTokens != warmUpTokens {
						return llm.ChatResult{}, errors.New("rejected")
					}
					return externalReply(ctx, req, p)
				}
				_, _ = h.runner(t, d).Run(context.Background())
				runs := len(h.store.ExchangesOf(domain.KindRun))
				if got := h.clock.Sleeps(); len(got) != runs-1 {
					t.Fatalf("%d requests, %d waits (%v); want %d waits", runs, len(got), got, runs-1)
				}
			})
		}
	}
}

func TestExternalFailureLimitDoesNotStopOtherModels(t *testing.T) {
	for _, order := range []domain.Order{domain.OrderModel, domain.OrderRoundRobin} {
		t.Run(string(order), func(t *testing.T) {
			h, f, d := newExternalHarness(domain.Model{Name: "bad1"}, domain.Model{Name: "good1"}, domain.Model{Name: "good2"})
			h.opts.Order = order
			h.opts.Runs = 5
			f.chat = func(ctx context.Context, req llm.ChatRequest, p llm.ProgressFunc) (llm.ChatResult, error) {
				if req.Model.Name == "bad1" && req.Options.MaxTokens != warmUpTokens {
					return llm.ChatResult{}, errors.New("request rejected")
				}
				return externalReply(ctx, req, p)
			}
			id, err := h.runner(t, d).Run(context.Background())
			if err == nil || h.status(t, id) != domain.StatusFailed || !strings.Contains(err.Error(), "stopped model bad1 after 3 consecutive failed steps") {
				t.Fatalf("Run: %s %v", id, err)
			}
			counts := map[string]int{}
			for _, x := range h.store.ExchangesOf(domain.KindRun) {
				counts[x.Model.Name]++
			}
			if counts["bad1"] != maxConsecutiveModelFailures || counts["good1"] != h.opts.Runs || counts["good2"] != h.opts.Runs {
				t.Fatal("model limit leaked across models", counts)
			}
		})
	}
}
