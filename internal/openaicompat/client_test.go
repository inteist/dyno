package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

const (
	roleChunk    = `{"choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}],"usage":null}`
	contentChunk = `{"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}],"usage":null}`
	finishChunk  = `{"choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":null}`
	usageChunk   = `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`
)

func frame(raw string) string { return "data: " + raw + "\n\n" }
func fullStream() string {
	return frame(roleChunk) + frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + frame("[DONE]")
}

func request() llm.ChatRequest {
	return llm.ChatRequest{
		Model: domain.Model{Name: "org/opaque:model"},
		Conversation: domain.Conversation{
			{Role: domain.RoleSystem, Content: "system"},
			{Role: domain.RoleUser, Content: "question"},
			{Role: domain.RoleAssistant, Content: "prior"},
			{Role: domain.RoleUser, Content: "next"},
		},
		Options:   llm.GenerationOptions{MaxTokens: 2, Temperature: 0, Seed: 0},
		KeepAlive: time.Minute, // ignored native default, not sent on the wire
	}
}

func newTestClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func streamServer(t *testing.T, payload string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		_, _ = io.WriteString(w, payload)
	}))
	t.Cleanup(s.Close)
	return s
}

func hostileEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range []string{"OPENAI_API_KEY", "OPENAI_ADMIN_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "OPENAI_WEBHOOK_SECRET"} {
		t.Setenv(name, "ambient-secret")
	}
	t.Setenv("OPENAI_BASE_URL", "https://invalid.example/paid/v1")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "Authorization: Bearer injected\nX-Injected: ambient-secret\nOpenAI-Organization: injected\nOpenAI-Project: injected")
}

func TestRequestAndEnvironmentIsolation(t *testing.T) {
	hostileEnvironment(t)
	for _, key := range []string{"", "resolved-key"} {
		for _, path := range []string{"/llm/v1", "/custom/api/"} {
			for _, limit := range []string{"", "max_completion_tokens"} {
				t.Run(fmt.Sprintf("key=%t/%s/%s", key != "", path, limit), func(t *testing.T) {
					var calls atomic.Int32
					s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls.Add(1)
						if r.Method != "POST" || r.URL.Path != strings.TrimSuffix(path, "/")+"/chat/completions" {
							t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
						}
						wantAuth := ""
						if key != "" {
							wantAuth = "Bearer " + key
						}
						if r.Header.Get("Authorization") != wantAuth {
							t.Error("resolved key not respected")
						}
						for _, h := range []string{"OpenAI-Organization", "OpenAI-Project", "X-Injected"} {
							if r.Header.Get(h) != "" {
								t.Errorf("ambient header %s", h)
							}
						}
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						field, other := "max_tokens", "max_completion_tokens"
						if limit != "" {
							field, other = other, field
						}
						if body[field] != float64(2) || body[other] != nil || body["n"] != float64(1) || body["stream"] != true || body["model"] != "org/opaque:model" {
							t.Errorf("request policy: %#v", body)
						}
						if body["seed"] != float64(0) || body["temperature"] != float64(0) {
							t.Error("explicit sampling zeros omitted")
						}
						if body["stream_options"].(map[string]any)["include_usage"] != true {
							t.Error("usage not requested")
						}
						for _, h := range []string{"num_ctx", "keep_alive", "truncate", "shift"} {
							if body[h] != nil {
								t.Errorf("native field %s sent", h)
							}
						}
						msgs := body["messages"].([]any)
						if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" || msgs[2].(map[string]any)["role"] != "assistant" {
							t.Error("messages not mapped")
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, fullStream())
					}))
					defer s.Close()
					c := newTestClient(t, Config{BaseURL: s.URL + path, APIKey: key, TokenLimitField: limit})
					var progress []int
					res, err := c.Chat(context.Background(), request(), func(n int) { progress = append(progress, n) })
					if err != nil {
						t.Fatal(err)
					}
					if res.Reply.Content != "hello" || res.Reply.Role != domain.RoleAssistant || res.Measurement.DoneReason != domain.DoneLength {
						t.Fatalf("bad result: %#v", res)
					}
					m := res.Measurement.Server
					if m.PromptTokens != 10 || m.EvalTokens != 2 || !m.Has(domain.MetricPromptTokens) || !m.Has(domain.MetricEvalTokens) {
						t.Fatalf("usage: %#v", m)
					}
					for _, metric := range []domain.ServerMetric{domain.MetricCachedTokens, domain.MetricEvalDuration, domain.MetricPromptDuration, domain.MetricLoadDuration, domain.MetricTotalDuration} {
						if m.Has(metric) {
							t.Errorf("invented metric %s", metric)
						}
					}
					if len(progress) != 1 || progress[0] != 1 || calls.Load() != 1 {
						t.Fatalf("progress=%v calls=%d", progress, calls.Load())
					}
					if res.Measurement.Client.TimeToFirstToken <= 0 || res.Measurement.Client.Wall < res.Measurement.Client.TimeToFirstToken {
						t.Fatal("bad client timings")
					}
				})
			}
		}
	}
}

func TestTimingOnlyAndExplicitOmissions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, field := range []string{"stream_options", "temperature", "seed"} {
			if _, ok := body[field]; ok {
				t.Errorf("omitted field %s present", field)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Even unsolicited usage cannot become authoritative in timing-only mode.
		_, _ = io.WriteString(w, fullStream())
	}))
	defer s.Close()
	c := newTestClient(t, Config{BaseURL: s.URL, Usage: "off", OmitSeed: true, OmitTemperature: true})
	res, err := c.Chat(context.Background(), request(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if *res.Measurement.Server.Available != (domain.MetricAvailability{}) {
		t.Fatal("timing-only invented counts")
	}
	if res.Measurement.Client.Wall <= 0 || res.Measurement.Client.TimeToFirstToken <= 0 {
		t.Fatal("timings unavailable")
	}
}

func TestPureFactsAndOpaqueIdentity(t *testing.T) {
	c := newTestClient(t, Config{BaseURL: "https://host.example/service/v1/", APIKey: "secret", ServerKind: "vllm", ServerVersion: "declared", SkipModelDiscovery: true})
	info, err := c.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Backend != domain.BackendOpenAI || info.ServerKind != "vllm" || info.Version != "declared" || info.VersionSource != "operator-supplied" || info.Endpoint != "https://host.example/service/v1" || info.ContextPolicy != "server-managed" || info.MeasurementProtocol != domain.ProtocolOpenAIChat {
		t.Fatalf("info: %#v", info)
	}
	if strings.Contains(info.ConfigJSON, "secret") || !strings.Contains(info.ConfigJSON, `"reasoning_policy":"unknown"`) {
		t.Fatal("unsafe provenance")
	}
	if version, err := c.Version(context.Background()); err != nil || version != "declared" {
		t.Fatal(version, err)
	}
	if _, ok := any(c).(llm.ManagedLifecycle); ok {
		t.Fatal("external client owns lifecycle")
	}
	if _, ok := any(c).(llm.CacheResetter); ok {
		t.Fatal("unverified cache reset")
	}
	name := "org/model:opaque"
	if c.NormalizeModelName(name) != name {
		t.Fatal("opaque ID changed")
	}
	models, err := c.Models(context.Background(), name)
	if err != nil || len(models) != 1 {
		t.Fatal(models, err)
	}
	if models[0].Identity != "" || models[0].Digest != "" || models[0].Revision != "" {
		t.Fatal("alias claimed weight identity")
	}
	cfg := Config{BaseURL: "https://host.example/service/v1", ModelRevision: "immutable", SkipModelDiscovery: true}
	rev := newTestClient(t, cfg)
	a, _ := rev.Models(context.Background(), name)
	b, _ := newTestClient(t, cfg).Models(context.Background(), name)
	if a[0].Identity != b[0].Identity || !strings.HasPrefix(a[0].Identity, domain.BackendOpenAI+":revision:") || a[0].IdentitySource != "operator-revision" || a[0].Digest != "" || a[0].Revision != "immutable" {
		t.Fatalf("revision identity: %#v", a)
	}
	cfg.ModelRevision = "changed"
	other, _ := newTestClient(t, cfg).Models(context.Background(), name)
	if a[0].Identity == other[0].Identity {
		t.Fatal("different revisions merged")
	}
	cfg.ModelRevision = "immutable"
	cfg.BaseURL = "https://other.example/service/v1"
	other, _ = newTestClient(t, cfg).Models(context.Background(), name)
	if a[0].Identity == other[0].Identity {
		t.Fatal("different endpoint scopes merged")
	}
	unknown := newTestClient(t, Config{})
	v, err := unknown.Version(context.Background())
	if err != nil || v != "" || unknown.info.VersionSource != "" || unknown.info.Endpoint != defaultBaseURL {
		t.Fatal("unknown version was probed/invented")
	}
}

func TestConfigAndOptionsValidation(t *testing.T) {
	for _, cfg := range []Config{
		{BaseURL: "relative/v1"}, {BaseURL: "ftp://host/v1"}, {BaseURL: "http://user:secret@host/v1"},
		{BaseURL: "https://host/v1?secret=x"}, {BaseURL: "https://host/v1?"}, {BaseURL: "https://host/v1#secret"},
		{BaseURL: "https://host/v1#"}, {BaseURL: "http://host:0/v1"}, {BaseURL: "http://host:65536/v1"},
		{Usage: "invalid"}, {TokenLimitField: "both"}, {ServerKind: "guess"}, {APIKey: "secret\nInjected: yes"},
	} {
		if _, err := New(cfg); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid config accepted or leaked: %v", err)
		}
	}
	c := newTestClient(t, Config{})
	if err := c.ValidateOptions(request().Options, []domain.CacheMode{domain.CacheMiss}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []domain.CacheMode{domain.CacheContinue, domain.CachePrefix, "invalid"} {
		if err := c.ValidateOptions(request().Options, []domain.CacheMode{mode}); err == nil {
			t.Fatal("unsupported mode accepted")
		}
	}
	for _, opts := range []llm.GenerationOptions{{MaxTokens: 1, ContextSize: 8192}, {MaxTokens: 1, ContextSize: -1}, {MaxTokens: 0}, {MaxTokens: 1, Temperature: 3}} {
		if err := c.ValidateOptions(opts, nil); err == nil {
			t.Fatal("unsupported options accepted")
		}
	}
}

func TestModelsDiscovery(t *testing.T) {
	hostileEnvironment(t)
	for _, code := range []int{200, 404, 405, 401, 403, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/prefix/models" || r.Method != "GET" {
					t.Errorf("bad models route %s", r.URL.Path)
				}
				if r.Header.Get("Authorization") != "" || r.Header.Get("X-Injected") != "" || r.Header.Get("OpenAI-Organization") != "" {
					t.Error("ambient credentials in discovery")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				if code == 200 {
					_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"org/model"},{"id":"alias:tag"}]}`)
				} else {
					_, _ = io.WriteString(w, `{"error":{"message":"secret","code":"denied"}}`)
				}
			}))
			defer s.Close()
			c := newTestClient(t, Config{BaseURL: s.URL + "/prefix"})
			models, err := c.Models(context.Background(), "org/model", "missing")
			switch code {
			case 200:
				if err != nil || len(models) != 1 || models[0].Name != "org/model" {
					t.Fatal(models, err)
				}
			case 404, 405:
				if err != nil || len(models) != 2 || models[1].Name != "missing" || models[0].Identity != "" {
					t.Fatal(models, err)
				}
				if _, err := c.Models(context.Background()); !errors.Is(err, ErrDiscoveryUnsupported) {
					t.Fatal("invented catalog", err)
				}
			default:
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != code || strings.Contains(err.Error(), "secret") {
					t.Fatal("auth/server failure hidden", err)
				}
			}
			if calls.Load() != 1 && code != 404 && code != 405 {
				t.Fatal("retry during discovery")
			}
			before := calls.Load()
			c = newTestClient(t, Config{BaseURL: s.URL + "/prefix", SkipModelDiscovery: true})
			models, err = c.Models(context.Background(), "org/model")
			if err != nil || len(models) != 1 || calls.Load() != before {
				t.Fatal("explicit discovery skip ignored", err)
			}
			if _, err := c.Models(context.Background()); !errors.Is(err, ErrDiscoveryUnsupported) {
				t.Fatal("skip invented catalog")
			}
		})
	}
}

func TestErrorsAndZeroRetries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		model  bool
	}{
		{"route", 404, `{"error":{"message":"Not Found"}}`, false},
		{"model-code", 404, `{"error":{"code":"model_not_found","message":"secret"}}`, true},
		{"vllm-model", 404, `{"error":{"code":"NotFoundError","message":"The model 'secret' does not exist."}}`, true},
		{"auth", 401, `{"error":{"message":"secret"}}`, false},
		{"permission", 403, `{"error":{"message":"secret"}}`, false},
		{"context", 400, `{"error":{"message":"secret context too long"}}`, false},
		{"rate", 429, `{"error":{"message":"secret"}}`, false},
		{"server", 500, `{"error":{"message":"secret"}}`, false},
		{"unready", 503, `{"error":{"message":"secret"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer s.Close()
			c := newTestClient(t, Config{BaseURL: s.URL, APIKey: "secret"})
			_, err := c.Chat(context.Background(), request(), nil)
			if err == nil || strings.Contains(fmt.Sprintf("%+v", err), "secret") || errors.Is(err, llm.ErrUnavailable) || calls.Load() != 1 {
				t.Fatal("unsafe error or retry", err, calls.Load())
			}
			if errors.Is(err, llm.ErrModelNotFound) != tc.model {
				t.Fatal("wrong missing-model mapping", err)
			}
			if !tc.model {
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != tc.status {
					t.Fatal("status not recognizable", err)
				}
			}
		})
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("secret dial failure")}
	})
	c := newTestClient(t, Config{HTTPClient: &http.Client{Transport: transport}})
	if _, err := c.Chat(context.Background(), request(), nil); !errors.Is(err, llm.ErrUnavailable) || strings.Contains(err.Error(), "secret") {
		t.Fatal("transport mapping", err)
	}
	if _, err := c.Models(context.Background(), "model"); !errors.Is(err, llm.ErrUnavailable) {
		t.Fatal("discovery transport hidden", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNoCredentialRedirect(t *testing.T) {
	for _, key := range []string{"", "secret"} {
		t.Run(fmt.Sprint(key != ""), func(t *testing.T) {
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); t.Error("redirect reached target") }))
			defer target.Close()
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target.URL+"/chat/completions", http.StatusTemporaryRedirect)
			}))
			defer origin.Close()
			var callerRedirect atomic.Int32
			hc := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { callerRedirect.Add(1); return nil }}
			c := newTestClient(t, Config{BaseURL: origin.URL, APIKey: key, HTTPClient: hc})
			_, err := c.Chat(context.Background(), request(), nil)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != 307 || redirected.Load() != 0 || callerRedirect.Load() != 0 {
				t.Fatal("redirect policy failed", err)
			}
			if hc.CheckRedirect == nil || hc.Transport != nil {
				t.Fatal("caller HTTP client mutated")
			}
		})
	}
}
