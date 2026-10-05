package domain

import (
	"strings"
	"testing"
	"time"
)

func TestServerMetricsRates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                  string
		m                     ServerMetrics
		decode, prefill, hits float64
	}{
		{
			name:    "typical",
			m:       ServerMetrics{PromptTokens: 1000, CachedTokens: 900, PromptDuration: 50 * time.Millisecond, EvalTokens: 512, EvalDuration: 8 * time.Second},
			decode:  64,
			prefill: 2000,
			hits:    0.9,
		},
		{
			name: "zero durations",
			m:    ServerMetrics{PromptTokens: 10, EvalTokens: 5},
		},
		{
			name:    "more cached than prompt",
			m:       ServerMetrics{PromptTokens: 10, CachedTokens: 12, PromptDuration: time.Second},
			prefill: 0,
			hits:    1.2,
		},
		{
			name: "empty prompt",
			m:    ServerMetrics{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.m.DecodeRate(); got != tt.decode {
				t.Errorf("DecodeRate() = %v, want %v", got, tt.decode)
			}
			if got := tt.m.PrefillRate(); got != tt.prefill {
				t.Errorf("PrefillRate() = %v, want %v", got, tt.prefill)
			}
			if got := tt.m.CachedShare(); got != tt.hits {
				t.Errorf("CachedShare() = %v, want %v", got, tt.hits)
			}
		})
	}
}

func TestWorkloadHash(t *testing.T) {
	t.Parallel()
	w := Workload{PromptName: "long", PromptHash: "abc", NumPredict: 512, NumCtx: 8192, Temperature: 0, Seed: 42}
	if got, want := w.Hash(), "21e02d1b0274"; got != want {
		t.Fatalf("Hash() = %q, want %q", got, want)
	}
	renamed := w
	renamed.PromptName = "other"
	if renamed.Hash() != w.Hash() {
		t.Error("the prompt name changed the hash")
	}
	for name, change := range map[string]func(*Workload){
		"prompt":      func(w *Workload) { w.PromptHash = "abd" },
		"num_predict": func(w *Workload) { w.NumPredict++ },
		"num_ctx":     func(w *Workload) { w.NumCtx++ },
		"temperature": func(w *Workload) { w.Temperature = 0.5 },
		"seed":        func(w *Workload) { w.Seed++ },
	} {
		changed := w
		change(&changed)
		if changed.Hash() == w.Hash() {
			t.Errorf("a change of %s kept the hash", name)
		}
	}
}

func TestConversationWithKeepsReceiver(t *testing.T) {
	t.Parallel()
	base := make(Conversation, 1, 4) // spare capacity would expose a shared array
	base[0] = Message{Role: RoleUser, Content: "q"}
	a := base.With(Message{Role: RoleAssistant, Content: "a"})
	b := base.With(Message{Role: RoleAssistant, Content: "b"})
	if len(base) != 1 {
		t.Fatalf("len(base) = %d, want 1", len(base))
	}
	if a[1].Content != "a" || b[1].Content != "b" {
		t.Fatalf("With shares memory: a=%v b=%v", a, b)
	}
}

func TestParseEnums(t *testing.T) {
	t.Parallel()
	for _, m := range CacheModes() {
		if got, err := ParseCacheMode(string(m)); err != nil || got != m {
			t.Errorf("ParseCacheMode(%q) = %q, %v", m, got, err)
		}
	}
	for _, o := range Orders() {
		if got, err := ParseOrder(string(o)); err != nil || got != o {
			t.Errorf("ParseOrder(%q) = %q, %v", o, got, err)
		}
	}
	_, err := ParseCacheMode("hit")
	if err == nil || !strings.Contains(err.Error(), "miss, continue, prefix") {
		t.Errorf("ParseCacheMode(hit) error = %v, want the list of modes", err)
	}
	if _, err := ParseOrder("random"); err == nil {
		t.Error("ParseOrder(random) gave no error")
	}
	if CacheMode("hit").Valid() || Order("").Valid() {
		t.Error("Valid() accepted an unknown value")
	}
}

func TestNewSessionID(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 10, 5, 16, 2, 12, 0, time.FixedZone("CEST", 2*3600))
	if got, want := NewSessionID(start, "K3F9A2QX"), SessionID("20261005T140212Z-K3F9A2"); got != want {
		t.Errorf("NewSessionID() = %q, want %q", got, want)
	}
	if got, want := NewSessionID(start, "AB"), SessionID("20261005T140212Z-AB"); got != want {
		t.Errorf("NewSessionID() with a short token = %q, want %q", got, want)
	}
}

func TestSmallMethods(t *testing.T) {
	t.Parallel()
	if got := (Model{Digest: "0123456789abcdef"}).ShortDigest(); got != "0123456789ab" {
		t.Errorf("ShortDigest() = %q", got)
	}
	if got := (Model{Digest: "abc"}).ShortDigest(); got != "abc" {
		t.Errorf("ShortDigest() of a short digest = %q", got)
	}
	if got := (Step{Model: "m", Mode: CacheMiss, Run: 2}).String(); got != "m miss run 2" {
		t.Errorf("Step.String() = %q", got)
	}
	if !(Exchange{Err: "boom"}).Failed() || (Exchange{}).Failed() {
		t.Error("Exchange.Failed() is wrong")
	}
}

func TestNormalizeModelName(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"lfm2.5":                        "lfm2.5:latest",
		"lfm2.5:8b":                     "lfm2.5:8b",
		"qwen3.5:9b-mlx":                "qwen3.5:9b-mlx",
		"team/model":                    "team/model:latest",
		"localhost:5000/team/model":     "localhost:5000/team/model:latest",
		"localhost:5000/team/model:q4":  "localhost:5000/team/model:q4",
		"":                              "",
		"registry.example.com/library/": "registry.example.com/library/",
	} {
		if got := NormalizeModelName(name); got != want {
			t.Errorf("NormalizeModelName(%q) = %q, want %q", name, got, want)
		}
	}
}
