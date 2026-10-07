package bench

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/event/eventtest"
	"dyno/internal/llm"
	"dyno/internal/llm/llmtest"
)

// externalFake deliberately implements only inference/discovery, not managed
// lifecycle methods (including successful no-op versions of those methods).
type externalFake struct {
	models    []domain.Model
	version   string
	calls     []string
	requested [][]string
	chats     []llm.ChatRequest
	chat      llmtest.ChatFunc
}

var _ llm.Client = (*externalFake)(nil)

func (f *externalFake) Version(ctx context.Context) (string, error) {
	f.calls = append(f.calls, "Version")
	return f.version, ctx.Err()
}

func (f *externalFake) Models(ctx context.Context, names ...string) ([]domain.Model, error) {
	f.calls = append(f.calls, "Models")
	f.requested = append(f.requested, slices.Clone(names))
	return slices.Clone(f.models), ctx.Err()
}

func (f *externalFake) Chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	f.calls = append(f.calls, "Chat")
	f.chats = append(f.chats, req)
	if f.chat != nil {
		return f.chat(ctx, req, progress)
	}
	return externalReply(ctx, req, progress)
}

func externalReply(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	if err := ctx.Err(); err != nil {
		return llm.ChatResult{}, err
	}
	if progress != nil {
		progress(1) // a content chunk, not a token count
	}
	return llm.ChatResult{
		Reply: domain.Message{Role: domain.RoleAssistant, Content: "hi"},
		Measurement: domain.Measurement{
			Server: domain.ServerMetrics{
				Available:    &domain.MetricAvailability{PromptTokens: true, EvalTokens: true},
				PromptTokens: len(req.Conversation), EvalTokens: req.Options.MaxTokens,
			},
			Client:     domain.ClientMetrics{TimeToFirstToken: 10 * time.Millisecond, Wall: 100 * time.Millisecond},
			DoneReason: domain.DoneLength,
		},
	}, nil
}

// externalPolicy exposes optional facts and policy validation independently of
// lifecycle ownership. Merely accepting a mode is not proof of cache reuse.
type externalPolicy struct {
	*externalFake
	info       llm.ServerInfo
	infoErr    error
	validation error
	validated  bool
	generation llm.GenerationOptions
	modes      []domain.CacheMode
}

func (f *externalPolicy) Info(ctx context.Context) (llm.ServerInfo, error) {
	f.calls = append(f.calls, "Info")
	return f.info, errors.Join(ctx.Err(), f.infoErr)
}

func (f *externalPolicy) ValidateOptions(g llm.GenerationOptions, modes []domain.CacheMode) error {
	f.validated, f.generation, f.modes = true, g, slices.Clone(modes)
	return f.validation
}

// resetClient tests only port dispatch, not a verified external cache profile.
type resetClient struct {
	*externalPolicy
	resets   []string
	resetErr error
}

func (f *resetClient) ResetCache(ctx context.Context, model string) error {
	f.calls = append(f.calls, "ResetCache")
	f.resets = append(f.resets, model)
	return errors.Join(ctx.Err(), f.resetErr)
}

func newExternalHarness(models ...domain.Model) (*harness, *externalFake, Deps) {
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.Name
	}
	h := newHarness(names...)
	h.opts.KeepAlive = 0
	h.opts.Generation.ContextSize = 0
	f := &externalFake{models: slices.Clone(models)}
	d := h.deps()
	d.Client = f
	return h, f, d
}

func TestExternalWarmupOncePerModel(t *testing.T) {
	t.Parallel()
	for _, order := range []domain.Order{domain.OrderModel, domain.OrderRoundRobin} {
		t.Run(string(order), func(t *testing.T) {
			t.Parallel()
			h, f, d := newExternalHarness(domain.Model{Name: "team/a"}, domain.Model{Name: "B:route"})
			if _, managed := d.Client.(llm.ManagedLifecycle); managed {
				t.Fatal("external fake implements managed lifecycle")
			}
			h.opts.Runs, h.opts.Order, h.opts.Interval = 3, order, time.Minute
			r := h.runner(t, d)
			if !r.opts.External || h.opts.External {
				t.Error("runner must detect external lifecycle without mutating caller options")
			}
			id, err := r.Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got := h.status(t, id); got != domain.StatusCompleted {
				t.Errorf("status = %s, want completed", got)
			}
			if len(f.requested) != 1 || !slices.Equal(f.requested[0], h.opts.Models) {
				t.Errorf("discovery = %v, want opaque IDs once", f.requested)
			}
			warmups := map[string]int{}
			for _, req := range f.chats {
				if req.Options.ContextSize != 0 || req.KeepAlive != 0 {
					t.Errorf("external request has native lifecycle settings: %+v", req)
				}
				if req.Options.MaxTokens == warmUpTokens {
					warmups[req.Model.Name]++
					want := domain.Conversation{{Role: domain.RoleUser, Content: warmUpPrompt}}
					if !slices.Equal(req.Conversation, want) {
						t.Errorf("warm-up = %v, want %v", req.Conversation, want)
					}
				}
			}
			if !maps.Equal(warmups, map[string]int{"team/a": 1, "B:route": 1}) || len(f.chats) != 8 {
				t.Errorf("warm-ups = %v, chats = %d, want one warm-up/model and six runs", warmups, len(f.chats))
			}
			if len(h.store.ExchangesOf(domain.KindWarmup)) != 2 || len(h.store.ExchangesOf(domain.KindRun)) != 6 || len(h.store.ExchangesOf(domain.KindPrimer)) != 0 {
				t.Errorf("exchanges = %+v", h.store.Exchanges())
			}
			for _, call := range f.calls {
				if call != "Version" && call != "Models" && call != "Chat" {
					t.Errorf("unexpected lifecycle call %s", call)
				}
			}
			for _, e := range h.events.Events() {
				switch e.(type) {
				case event.ModelLoading, event.ModelReady, event.ModelReleased:
					t.Errorf("external endpoint ownership event: %+v", e)
				}
			}
			if len(h.clock.Sleeps()) != 5 {
				t.Errorf("sleeps = %v, want scheduling waits only", h.clock.Sleeps())
			}
			rec, _ := h.store.Session(id)
			s := rec.Session
			if s.BackendName() != "unknown" || s.Protocol() != "unknown" || s.ServerKind != "unknown" || s.ServerVersion != "" || s.ContextPolicy != "server-managed" || s.Workload.NumCtx != 0 || s.Host.Name != "mac" {
				t.Errorf("metadata-free external session must not imply Ollama or remote host facts: %+v", s)
			}
			for _, x := range h.store.Exchanges() {
				if x.Measurement.Server.Has(domain.MetricCachedTokens) || x.Measurement.Server.Has(domain.MetricEvalDuration) {
					t.Error("external exchange invented server timings or cache statistics")
				}
			}
		})
	}
}

func TestExternalSessionInfo(t *testing.T) {
	t.Parallel()
	h, f, d := newExternalHarness(domain.Model{Name: "alias"})
	f.version = "endpoint-version"
	info := llm.ServerInfo{
		Backend: "openai", ServerKind: "vllm", Version: "operator-version", VersionSource: "operator",
		Endpoint: "https://llm.example/api/v1", MeasurementProtocol: "openai-client-v1",
		ContextPolicy: "server-managed", ConfigJSON: `{"usage":"required","cache_policy":"unverified"}`,
	}
	policy := &externalPolicy{externalFake: f, info: info}
	d.Client = policy
	id, err := h.runner(t, d).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := h.store.Session(id)
	s := rec.Session
	if s.Backend != info.Backend || s.ServerKind != info.ServerKind || s.ServerVersion != info.Version || s.VersionSource != info.VersionSource || s.Endpoint != info.Endpoint || s.MeasurementProtocol != info.MeasurementProtocol || s.ContextPolicy != info.ContextPolicy || s.ConfigJSON != info.ConfigJSON {
		t.Errorf("session provenance = %+v, want %+v", s, info)
	}
	if !policy.validated || policy.generation != h.opts.Generation || !slices.Equal(policy.modes, h.opts.CacheModes) {
		t.Errorf("adapter validation = %t, %+v, %v", policy.validated, policy.generation, policy.modes)
	}
}

func TestExternalInfoErrorWritesNothing(t *testing.T) {
	t.Parallel()
	h, f, d := newExternalHarness(domain.Model{Name: "alias"})
	boom := errors.New("provenance unavailable")
	d.Client = &externalPolicy{externalFake: f, infoErr: boom}
	id, err := h.runner(t, d).Run(context.Background())
	if id != "" || !errors.Is(err, boom) || !strings.Contains(err.Error(), "server info") {
		t.Errorf("Run() = %q, %v, want info error", id, err)
	}
	h.assertNothingWritten(t)
	if !slices.Equal(f.calls, []string{"Version", "Info"}) {
		t.Errorf("calls = %v, want Version and Info only", f.calls)
	}
}

func TestExternalModelIdentity(t *testing.T) {
	t.Parallel()
	fixed := domain.Model{Name: "fixed", Identity: "openai:fixed:rev1", IdentitySource: "operator-revision", Revision: "rev1"}
	known := domain.Model{Name: "known", Digest: "sha256:known"}
	h, f, d := newExternalHarness(domain.Model{Name: "alias/a"}, domain.Model{Name: "alias/b"}, fixed, known)
	r := h.runner(t, d)
	var ids []domain.SessionID
	for range 2 {
		id, err := r.Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if ids[0] == ids[1] {
		t.Fatal("sessions have the same ID")
	}
	stored := map[string]domain.Model{}
	for _, m := range h.store.Models() {
		stored[m.StorageKey()] = m
	}
	if len(stored) != 6 || stored[fixed.StorageKey()] != fixed || stored[known.StorageKey()] != known {
		t.Errorf("stored models = %v, want four session aliases plus fixed revision and digest", stored)
	}
	for _, id := range ids {
		for _, name := range []string{"alias/a", "alias/b"} {
			key := fmt.Sprintf("session:%s:%s", id, name)
			m, ok := stored[key]
			if !ok || m.Name != name || m.Digest != "" || m.IdentitySource != "session" || m.Revision != "" {
				t.Errorf("model %q = %+v, %t", key, m, ok)
			}
		}
	}
	for _, x := range h.store.Exchanges() {
		if m, ok := stored[x.Model.StorageKey()]; !ok || m != x.Model {
			t.Errorf("exchange model has no matching full stored identity: %+v", x)
		}
	}
	if f.models[0].Identity != "" || f.models[1].Identity != "" {
		t.Error("runner changed adapter-owned model metadata")
	}
}

func TestAdapterOptionsRejectedBeforeSession(t *testing.T) {
	t.Parallel()
	h, f, d := newExternalHarness(domain.Model{Name: "alias"})
	boom := errors.New("sampling policy not supported")
	policy := &externalPolicy{externalFake: f, validation: boom}
	d.Client = policy
	r, err := NewRunner(d, h.opts)
	if r != nil || !errors.Is(err, boom) || !errors.Is(err, ErrInvalidOptions) || !policy.validated {
		t.Errorf("NewRunner() returned runner=%t, error=%v, validation=%t", r != nil, err, policy.validated)
	}
	h.assertNothingWritten(t)
	if len(f.calls) != 0 {
		t.Errorf("network calls before option validation: %v", f.calls)
	}
}

func TestExternalCacheModePolicies(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		mode       domain.CacheMode
		resetter   bool
		adapterErr error
		want       string
	}{
		{"continue", domain.CacheContinue, false, nil, "not implemented"},
		{"prefix without reset", domain.CachePrefix, false, nil, "real cache reset"},
		{"continue even with reset", domain.CacheContinue, true, nil, "not implemented"},
		{"adapter rejects prefix", domain.CachePrefix, true, errors.New("unvalidated prefix policy"), "unvalidated prefix policy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, f, d := newExternalHarness(domain.Model{Name: "alias"})
			h.opts.CacheModes = []domain.CacheMode{tt.mode}
			policy := &externalPolicy{externalFake: f, validation: tt.adapterErr}
			d.Client = policy
			if tt.resetter {
				d.Client = &resetClient{externalPolicy: policy}
			}
			r, err := NewRunner(d, h.opts)
			if r != nil || !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("NewRunner() returned runner=%t, error=%v, want %q", r != nil, err, tt.want)
			}
			h.assertNothingWritten(t)
			if len(f.calls) != 0 {
				t.Errorf("calls = %v, want no probes or resets", f.calls)
			}
		})
	}
}

func TestExternalResetCachePort(t *testing.T) {
	t.Parallel()
	boom := errors.New("reset refused")
	for _, tt := range []struct {
		name string
		mode domain.CacheMode
		err  error
		want []string
	}{
		{"miss does not reset", domain.CacheMiss, nil, []string{"Version", "Info", "Models", "Chat", "Chat"}},
		{"prefix resets before primers", domain.CachePrefix, nil, []string{"Version", "Info", "Models", "Chat", "ResetCache", "Chat", "Chat", "Chat"}},
		{"failed reset stops preparation", domain.CachePrefix, boom, []string{"Version", "Info", "Models", "Chat", "ResetCache"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, f, d := newExternalHarness(domain.Model{Name: "alias"})
			h.opts.CacheModes = []domain.CacheMode{tt.mode}
			reset := &resetClient{externalPolicy: &externalPolicy{externalFake: f}, resetErr: tt.err}
			d.Client = reset
			_, err := h.runner(t, d).Run(context.Background())
			if (err != nil) != (tt.err != nil) {
				t.Errorf("Run() = %v, reset error = %v", err, tt.err)
			}
			if !slices.Equal(f.calls, tt.want) {
				t.Errorf("calls = %v, want %v", f.calls, tt.want)
			}
			if tt.mode == domain.CacheMiss && len(reset.resets) != 0 || tt.mode == domain.CachePrefix && !slices.Equal(reset.resets, []string{"alias"}) {
				t.Errorf("resets = %v", reset.resets)
			}
			if tt.err != nil {
				failed := eventtest.Of[event.StepFailed](h.events)
				if len(failed) != 1 || !errors.Is(failed[0].Err, boom) || !strings.Contains(failed[0].Err.Error(), "reset cache") {
					t.Errorf("StepFailed = %+v, want reset error", failed)
				}
			}
		})
	}
}

func TestExternalFailurePolicy(t *testing.T) {
	t.Parallel()
	boom := errors.New("request rejected")
	for _, tt := range []struct {
		name   string
		warmup bool
		err    error
		status domain.SessionStatus
		failed int
		runs   int
	}{
		{"warm-up rejection finalizes", true, boom, domain.StatusFailed, 1, 0},
		{"warm-up unavailable aborts", true, llm.ErrUnavailable, domain.StatusFailed, 0, 0},
		{"run rejection continues", false, boom, domain.StatusCompleted, 1, 2},
		{"run unavailable aborts", false, llm.ErrUnavailable, domain.StatusFailed, 0, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, f, d := newExternalHarness(domain.Model{Name: "alias"})
			h.opts.Runs = 2
			failedOnce := false
			f.chat = func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
				warmup := req.Options.MaxTokens == warmUpTokens
				if warmup == tt.warmup && !failedOnce {
					failedOnce = true
					return llm.ChatResult{}, tt.err
				}
				return externalReply(ctx, req, progress)
			}
			id, err := h.runner(t, d).Run(context.Background())
			if err == nil || h.status(t, id) != tt.status || h.ended(t).Failed != tt.failed {
				t.Errorf("Run() = %q, %v; SessionEnded = %+v", id, err, h.ended(t))
			}
			if tt.status == domain.StatusFailed && !errors.Is(err, tt.err) {
				t.Errorf("Run() = %v, want readiness/unavailability cause %v", err, tt.err)
			}
			if len(h.store.ExchangesOf(domain.KindWarmup)) != 1 || len(h.store.ExchangesOf(domain.KindRun)) != tt.runs {
				t.Errorf("exchanges = %+v", h.store.Exchanges())
			}
		})
	}
}

func TestExternalTimeoutAndCancellation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		cancel bool
		warmup bool
	}{
		{"request timeout continues", false, false},
		{"warm-up timeout finalizes", false, true},
		{"parent cancellation during run", true, false},
		{"parent cancellation during warm-up", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h, f, d := newExternalHarness(domain.Model{Name: "alias"})
			h.opts.Runs, h.opts.RequestTimeout = 2, 20*time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			blockedOnce := false
			f.chat = func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
				if (req.Options.MaxTokens == warmUpTokens) != tt.warmup || blockedOnce {
					return externalReply(ctx, req, progress)
				}
				blockedOnce = true
				if tt.cancel {
					cancel()
				}
				<-ctx.Done()
				return llm.ChatResult{}, ctx.Err()
			}
			id, err := h.runner(t, d).Run(ctx)
			if tt.cancel {
				if !errors.Is(err, context.Canceled) || h.status(t, id) != domain.StatusAborted {
					t.Errorf("Run() = %v, want durable aborted session", err)
				}
				want := 1
				if tt.warmup {
					want = 0
				}
				if len(h.store.Exchanges()) != want {
					t.Errorf("exchanges = %+v, canceled request must not be recorded", h.store.Exchanges())
				}
			} else {
				wantStatus := domain.StatusCompleted
				if tt.warmup {
					wantStatus = domain.StatusFailed
				}
				if err == nil || h.status(t, id) != wantStatus || h.ended(t).Failed != 1 {
					t.Errorf("Run() = %v, SessionEnded = %+v", err, h.ended(t))
				}
				var failures int
				for _, x := range h.store.Exchanges() {
					if x.Failed() {
						failures++
						if !strings.Contains(x.Err, "no answer within the request timeout 20ms") {
							t.Errorf("timeout error = %q", x.Err)
						}
					}
				}
				if failures != 1 {
					t.Errorf("%d recorded failures, want one request timeout", failures)
				}
			}
		})
	}
}

// nativeNaming supplies Ollama's naming convention independently of lifecycle.
type nativeNaming struct{ *llmtest.Fake }

func (nativeNaming) NormalizeModelName(name string) string { return domain.NormalizeModelName(name) }

func TestRunnerNormalizesCopiedOptions(t *testing.T) {
	t.Parallel()
	h := newHarness("a:latest")
	h.opts.Models = []string{" a "}
	before := slices.Clone(h.opts.Models)
	d := h.deps()
	d.Client = nativeNaming{h.client}
	r := h.runner(t, d)
	if !slices.Equal(r.opts.Models, []string{"a:latest"}) || !slices.Equal(h.opts.Models, before) {
		t.Errorf("runner names = %v, caller names = %v", r.opts.Models, h.opts.Models)
	}
	h.opts.Models[0], h.opts.CacheModes[0] = "changed", domain.CachePrefix
	if _, err := r.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(r.opts.CacheModes, []domain.CacheMode{domain.CacheMiss}) {
		t.Error("runner shares caller's cache-mode slice")
	}
	for _, c := range h.client.CallsOf(llmtest.MethodChat) {
		if c.Model != "a:latest" {
			t.Errorf("chat model = %q, want a:latest", c.Model)
		}
	}
}

func TestRunnerRejectsCanonicalDuplicates(t *testing.T) {
	t.Parallel()
	h := newHarness("a:latest")
	h.opts.Models = []string{"a", "a:latest"}
	d := h.deps()
	d.Client = nativeNaming{h.client}
	r, err := NewRunner(d, h.opts)
	if r != nil || !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "model a:latest occurs more than once") {
		t.Errorf("NewRunner() returned runner=%t, error=%v, want duplicate canonical names", r != nil, err)
	}
	if !reflect.DeepEqual(h.opts.Models, []string{"a", "a:latest"}) {
		t.Error("normalization mutated caller's model names")
	}
	h.assertNothingWritten(t)
}

func TestManagedClientCannotBypassNativeValidation(t *testing.T) {
	t.Parallel()
	h := newHarness("a")
	h.opts.External, h.opts.Generation.ContextSize, h.opts.KeepAlive = true, 0, 0
	r, err := NewRunner(h.deps(), h.opts)
	if r != nil || !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "num_ctx must be 1 or more") || !strings.Contains(err.Error(), "keep-alive") {
		t.Errorf("NewRunner() returned runner=%t, error=%v, want strict managed validation", r != nil, err)
	}
}
