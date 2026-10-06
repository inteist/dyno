package bench

import (
	"errors"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/llm"
	"dyno/internal/prompt"
)

func validOptions() Options {
	long, _ := prompt.Builtin(prompt.Long)
	return Options{
		Models:         []string{"a", "b"},
		Runs:           3,
		Interval:       30 * time.Second,
		Order:          domain.OrderModel,
		CacheModes:     domain.CacheModes(),
		Prompt:         long,
		Generation:     llm.GenerationOptions{MaxTokens: 512, ContextSize: 8192, Seed: 42},
		KeepAlive:      DefaultKeepAlive(30 * time.Second),
		RequestTimeout: 10 * time.Minute,
	}
}

func TestValidateAcceptsValidOptions(t *testing.T) {
	t.Parallel()
	if err := validOptions().Validate(); err != nil {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestValidateRules(t *testing.T) {
	t.Parallel()
	short, _ := prompt.Builtin(prompt.Short)
	tests := []struct {
		name   string
		change func(*Options)
		want   string
	}{
		{"no model", func(o *Options) { o.Models = nil }, "at least one model"},
		{"empty model", func(o *Options) { o.Models = []string{"a", ""} }, "model name is empty"},
		{"model twice", func(o *Options) { o.Models = []string{"a", "a"} }, "model a occurs more than once"},
		{"zero runs", func(o *Options) { o.Runs = 0 }, "runs must be 1 or more"},
		{"negative interval", func(o *Options) { o.Interval = -time.Second }, "interval must not be negative"},
		{"bad order", func(o *Options) { o.Order = "random" }, `unknown order "random"`},
		{"no mode", func(o *Options) { o.CacheModes = nil }, "at least one cache mode"},
		{"mode twice", func(o *Options) { o.CacheModes = []domain.CacheMode{"miss", "miss"} }, "cache mode miss occurs more than once"},
		{"unknown mode", func(o *Options) { o.CacheModes = []domain.CacheMode{"hit"} }, `unknown cache mode "hit"`},
		{"prompt without system", func(o *Options) { o.Prompt = short }, `prompt "short" cannot serve the cache mode continue`},
		{"short keep-alive", func(o *Options) { o.KeepAlive = o.Interval }, "keep-alive 30s must be longer than the interval 30s"},
		{"no tokens", func(o *Options) { o.Generation.MaxTokens = 0 }, "num_predict must be 1 or more"},
		{"no context", func(o *Options) { o.Generation.ContextSize = 0 }, "num_ctx must be 1 or more"},
		{"negative temperature", func(o *Options) { o.Generation.Temperature = -1 }, "temperature must not be negative"},
		{"no timeout", func(o *Options) { o.RequestTimeout = 0 }, "request timeout must be longer than 0"},
		{"negative progress", func(o *Options) { o.ProgressEvery = -1 }, "progress interval must not be negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := validOptions()
			tt.change(&o)
			err := o.Validate()
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("Validate() = %v, want ErrInvalidOptions", err)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Validate() = %q, want %q in it", err, tt.want)
			}
		})
	}
}

func TestValidateReportsAllProblems(t *testing.T) {
	t.Parallel()
	o := validOptions()
	o.Models, o.Runs, o.Generation.MaxTokens = nil, 0, 0
	err := o.Validate()
	for _, want := range []string{"at least one model", "runs must be", "num_predict"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %v, want %q in it", err, want)
		}
	}
}

func TestDefaultKeepAlive(t *testing.T) {
	t.Parallel()
	if got := DefaultKeepAlive(30 * time.Second); got != 10*time.Minute {
		t.Errorf("DefaultKeepAlive(30s) = %s, want 10m", got)
	}
	if got := DefaultKeepAlive(20 * time.Minute); got != 25*time.Minute {
		t.Errorf("DefaultKeepAlive(20m) = %s, want 25m", got)
	}
}

func TestWorkloadAndProgressEvery(t *testing.T) {
	t.Parallel()
	o := validOptions()
	w := o.Workload()
	if w.PromptName != prompt.Long || w.PromptHash != o.Prompt.Hash() || w.NumPredict != 512 || w.NumCtx != 8192 || w.Seed != 42 {
		t.Errorf("Workload() = %+v", w)
	}
	if o.progressEvery() != DefaultProgressEvery {
		t.Errorf("progressEvery() = %s, want the default", o.progressEvery())
	}
	o.ProgressEvery = time.Second
	if o.progressEvery() != time.Second {
		t.Errorf("progressEvery() = %s, want 1s", o.progressEvery())
	}
}
