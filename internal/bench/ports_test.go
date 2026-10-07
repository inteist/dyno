package bench

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"dyno/internal/domain"
	"dyno/internal/llm"
	"dyno/internal/llm/llmtest"
)

// managedInfoClient supplies facts without changing the native lifecycle.
type managedInfoClient struct {
	*llmtest.Fake
	info llm.ServerInfo
}

func (f managedInfoClient) Info(context.Context) (llm.ServerInfo, error) { return f.info, nil }

func TestManagedSessionInfo(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	info := llm.ServerInfo{
		Backend: "ollama", ServerKind: "ollama", VersionSource: "api",
		MeasurementProtocol: "ollama-server-v1", ContextPolicy: "request-controlled",
		ConfigJSON: `{"reasoning_policy":"disabled-when-capable"}`,
	}
	d := h.deps()
	d.Client = managedInfoClient{Fake: h.client, info: info}
	id, err := h.runner(t, d).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := h.store.Session(id)
	s := rec.Session
	if s.ServerVersion != "0.34.0" || s.Backend != info.Backend || s.ServerKind != info.ServerKind || s.VersionSource != info.VersionSource || s.MeasurementProtocol != info.MeasurementProtocol || s.ContextPolicy != info.ContextPolicy || s.ConfigJSON != info.ConfigJSON {
		t.Errorf("session facts = %+v, want native version plus optional info", s)
	}
	if len(h.client.CallsOf(llmtest.MethodLoad)) != 1 || len(h.client.CallsOf(llmtest.MethodUnload)) != 1 {
		t.Error("optional info changed native ownership")
	}
}

// managedResetClient must still use the native reload/warm-up mechanism.
type managedResetClient struct {
	*llmtest.Fake
	resets int
}

func (f *managedResetClient) ResetCache(context.Context, string) error {
	f.resets++
	return errors.New("must use native reload")
}

func TestManagedResetCachePreservesReload(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.CacheModes = []domain.CacheMode{domain.CachePrefix}
	client := &managedResetClient{Fake: h.client}
	d := h.deps()
	d.Client = client
	if _, err := h.runner(t, d).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.resets != 0 || len(h.client.CallsOf(llmtest.MethodLoad)) != 2 || len(h.client.CallsOf(llmtest.MethodUnload)) != 2 || len(h.store.ExchangesOf(domain.KindWarmup)) != 2 || len(h.store.ExchangesOf(domain.KindPrimer)) != 2 {
		t.Errorf("native reset: %d external resets, calls = %v, exchanges = %+v", client.resets, methods(h.client), h.store.Exchanges())
	}
}

func TestExternalContextRequiresAdapterPolicy(t *testing.T) {
	t.Parallel()
	h, f, d := newExternalHarness(domain.Model{Name: "alias"})
	h.opts.Generation.ContextSize = 4096
	r, err := NewRunner(d, h.opts)
	if r != nil || !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "server-managed") {
		t.Errorf("NewRunner() returned runner=%t, error=%v, want unsupported context policy", r != nil, err)
	}
	h.assertNothingWritten(t)
	if len(f.calls) != 0 {
		t.Errorf("unsupported options made requests: %v", f.calls)
	}
	// An adapter/profile may explicitly validate an enforceable context policy.
	policy := &externalPolicy{externalFake: f}
	d.Client = policy
	r, err = NewRunner(d, h.opts)
	if err != nil || !policy.validated || policy.generation.ContextSize != 4096 {
		t.Fatalf("validated context policy: error=%v, options=%+v", err, policy.generation)
	}
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// changingValidator verifies that policy validation cannot alter runner options.
type changingValidator struct{ *externalFake }

func (changingValidator) ValidateOptions(_ llm.GenerationOptions, modes []domain.CacheMode) error {
	modes[0] = domain.CachePrefix
	return nil
}

func TestAdapterValidationCannotMutateCacheModes(t *testing.T) {
	t.Parallel()
	h, f, d := newExternalHarness(domain.Model{Name: "alias"})
	d.Client = changingValidator{f}
	r := h.runner(t, d)
	if !slices.Equal(r.opts.CacheModes, []domain.CacheMode{domain.CacheMiss}) || !slices.Equal(h.opts.CacheModes, []domain.CacheMode{domain.CacheMiss}) {
		t.Error("adapter validator mutated runner or caller cache modes")
	}
}

func TestExternalResetWithoutCapabilityFails(t *testing.T) {
	t.Parallel()
	h, _, d := newExternalHarness(domain.Model{Name: "alias"})
	r := h.runner(t, d)
	model := domain.Model{Name: "alias"}
	s := r.newSessionRun("s", []domain.Model{model})
	err := s.executor(domain.Step{Model: model.Name, Mode: domain.CachePrefix, Run: 1}, model, 2).ResetCache(context.Background())
	if err == nil || !strings.Contains(err.Error(), "real cache reset") {
		t.Errorf("ResetCache() = %v, want missing capability error", err)
	}
}
