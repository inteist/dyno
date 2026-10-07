package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
)

type smallReads struct {
	io.Reader
	size int
}

func (r smallReads) Read(p []byte) (int, error) {
	if len(p) > r.size {
		p = p[:r.size]
	}
	return r.Reader.Read(p)
}

func TestLeadingBOMPreservesFirstSSEContent(t *testing.T) {
	for _, size := range []int{1, 2, 4096} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			payload := "\xef\xbb\xbf" + frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + frame("[DONE]")
			c := newTestClient(t, Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(smallReads{strings.NewReader(payload), size}), Request: r}, nil
			})}})
			base, tick := time.Now(), 0
			c.now = func() time.Time { now := base.Add(time.Duration(tick) * time.Second); tick++; return now }
			var progress []int
			res, err := c.Chat(context.Background(), request(), func(n int) { progress = append(progress, n) })
			if err != nil || res.Reply.Content != "hello" || len(progress) != 1 || res.Measurement.Client.TimeToFirstToken != time.Second {
				t.Fatal("BOM lost first content/timing", res, progress, err)
			}
		})
	}
}

func TestReasoningDeltasAreNotVisibleContentOrInferredTokens(t *testing.T) {
	for _, visible := range []bool{false, true} {
		t.Run(fmt.Sprint(visible), func(t *testing.T) {
			payload := frame(roleChunk) + frame(`{"choices":[{"index":0,"delta":{"reasoning_content":"private thought","reasoning":"another private thought"},"finish_reason":null}]}`)
			if visible {
				payload += frame(contentChunk)
			}
			payload += frame(finishChunk) + frame(`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"completion_tokens_details":{"reasoning_tokens":2}}}`) + frame("[DONE]")
			s := streamServer(t, payload)
			c := newTestClient(t, Config{BaseURL: s.URL})
			var policy map[string]any
			if err := json.Unmarshal([]byte(c.info.ConfigJSON), &policy); err != nil {
				t.Fatal(err)
			}
			if policy[domain.TTFTBasisKey] != domain.TTFTBasisVisibleContent || policy[domain.ReasoningPolicyKey] != "unknown" {
				t.Fatal("TTFT basis not persisted", policy)
			}
			base, tick := time.Now(), 0
			c.now = func() time.Time { now := base.Add(time.Duration(tick) * time.Second); tick++; return now }
			progress := 0
			res, err := c.Chat(context.Background(), request(), func(int) { progress++ })
			if err != nil {
				t.Fatal(err)
			}
			wantTTFT, wantProgress, wantReply := time.Duration(0), 0, ""
			if visible {
				wantTTFT, wantProgress, wantReply = 3*time.Second, 1, "hello"
			}
			if res.Measurement.Client.TimeToFirstToken != wantTTFT || progress != wantProgress || res.Reply.Content != wantReply || res.Measurement.Server.EvalTokens != 2 {
				t.Fatal("reasoning counted as content/tokens", res, progress)
			}
		})
	}
}

func TestFinishChunkUsageRemainsOutsideStrictContract(t *testing.T) {
	payload := frame(contentChunk) + frame(`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`) + frame("[DONE]")
	s := streamServer(t, payload)
	_, err := newTestClient(t, Config{BaseURL: s.URL}).Chat(context.Background(), request(), nil)
	if !errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "usage must be a final chunk after finish") {
		t.Fatal("finish-chunk usage silently accepted", err)
	}
}

func TestActualGuardDetailsSurviveSDKErrorMapping(t *testing.T) {
	for _, tc := range []struct{ name, payload, contentType, detail string }{
		{"event-limit", "data: " + strings.Repeat("x", maxEventBytes+1) + "\n\n", "text/event-stream", "stream size limit exceeded"},
		{"media-type", fullStream(), "application/json", "expected text/event-stream"},
		{"error-event", "event: error\ndata: {\"message\":\"resolved-key\"}\n\n", "text/event-stream", "error event"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.payload)), Request: r}, nil
			})}})
			_, err := c.Chat(context.Background(), request(), nil)
			if !errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), tc.detail) {
				t.Fatal("guard detail replaced by generic SDK error", err)
			}
			assertSafeError(t, err, "resolved-key")
		})
	}
}

func TestCanonicalEndpointIdentityAndEncodedSlash(t *testing.T) {
	for _, tc := range []struct{ raw, canonical string }{
		{"https://HOST.Example:443/service/v1/", "https://host.example/service/v1"},
		{"http://HOST.Example:00080/service/v1", "http://host.example/service/v1"},
		{"http://[::1]:80/service/v1/", "http://[::1]/service/v1"},
		{"https://HOST.Example:8443/proxy%2Fprefix/api%2F", "https://host.example:8443/proxy%2Fprefix/api%2F"},
	} {
		c := newTestClient(t, Config{BaseURL: tc.raw, ModelRevision: "immutable", SkipModelDiscovery: true})
		other := newTestClient(t, Config{BaseURL: tc.canonical, ModelRevision: "immutable", SkipModelDiscovery: true})
		if c.info.Endpoint != tc.canonical || c.config.BaseURL != tc.canonical || c.model("model").Identity != other.model("model").Identity {
			t.Fatal("noncanonical endpoint/identity", c.info.Endpoint)
		}
	}
	for _, raw := range []string{"http://host:/v1", "http://host:65536/v1", "http://host/v1%00secret", "http://host/v1\x7fsecret"} {
		_, err := New(Config{BaseURL: raw})
		assertSafeError(t, err, "secret")
	}
	// A terminal encoded slash is part of the prefix, not the SDK separator.
	c := newTestClient(t, Config{BaseURL: "http://HOST.test/proxy%2Fprefix/api%2F", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.EscapedPath() != "/proxy%2Fprefix/api%2F/chat/completions" {
			t.Error("encoded slash dropped", r.URL.EscapedPath())
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(fullStream())), Request: r}, nil
	})}})
	if _, err := c.Chat(context.Background(), request(), nil); err != nil {
		t.Fatal(err)
	}
}

// Strict schema keeps the manifest executable rather than a stale prose note.
type sdkContract struct {
	SDK                   string `json:"sdk"`
	Version               string `json:"version"`
	MinimumGo             string `json:"sdk_minimum_go"`
	Validation            string `json:"validation"`
	Construction          string `json:"client_construction"`
	API                   string `json:"inference_api"`
	Retries               int    `json:"retries"`
	Redirects             string `json:"redirects"`
	Terminal              string `json:"terminal_contract"`
	UsageRequired         string `json:"usage_required"`
	CachedTokens          string `json:"cached_tokens"`
	UsageOff              string `json:"usage_off"`
	Timing                string `json:"timing"`
	DefaultTokenLimit     string `json:"default_token_limit_field"`
	AlternativeTokenLimit string `json:"alternative_token_limit_field"`
	Limits                struct {
		Event     int `json:"event"`
		ErrorBody int `json:"error_body"`
		Models    int `json:"model_list_body"`
		Stream    int `json:"stream"`
		Reply     int `json:"reply"`
	} `json:"limits_bytes"`
	DiscoveryTimeout int    `json:"discovery_timeout_seconds"`
	DiagnosticChars  int    `json:"diagnostic_characters"`
	TTFTBasis        string `json:"ttft_basis"`
	UserAgent        string `json:"user_agent"`
	Telemetry        string `json:"sdk_telemetry"`
	Generation       string `json:"generation_defaults"`
	CacheModes       string `json:"cache_modes"`
	LiveValidation   string `json:"live_vllm_validation"`
	Fixtures         string `json:"fixtures"`
	DependencyNote   string `json:"dependency_note"`
}

func TestSDKContractManifestMatchesInstalledSDKAndAdapter(t *testing.T) {
	file, err := os.Open("testdata/sdk-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var contract sdkContract
	if err := decoder.Decode(&contract); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("manifest has trailing data", err)
	}
	if contract.SDK != "github.com/openai/openai-go/v3" || contract.Version == "" {
		t.Fatal("invalid SDK manifest")
	}
	installed := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range info.Deps {
			if dep.Path == contract.SDK {
				if dep.Replace != nil {
					t.Fatal("SDK replacement requires contract revalidation")
				}
				installed = dep.Version
			}
		}
	}
	// Go test binaries may omit module dependency build info; the module manifest
	// is then the authoritative offline version (no `go list` network resolution).
	if installed == "" {
		mod, err := os.ReadFile("../../go.mod")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(mod), "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[0] == contract.SDK {
				installed = fields[1]
			}
			if len(fields) > 0 && fields[0] == "replace" && strings.Contains(line, contract.SDK) {
				t.Fatal("SDK replacement requires contract revalidation")
			}
		}
	}
	if installed != contract.Version {
		t.Fatalf("installed SDK %q differs from validated %q", installed, contract.Version)
	}
	if contract.Limits.Event != maxEventBytes || contract.Limits.ErrorBody != maxErrorBytes || contract.Limits.Models != maxModelsBytes || contract.Limits.Stream != maxStreamBytes || contract.Limits.Reply != maxReplyBytes {
		t.Fatal("framing/body limits differ from manifest")
	}
	c := newTestClient(t, Config{})
	if contract.DefaultTokenLimit != c.config.TokenLimitField || contract.AlternativeTokenLimit != "max_completion_tokens" || contract.Retries != 0 || contract.DiscoveryTimeout != int(c.config.DiscoveryTimeout/time.Second) || contract.DiagnosticChars != maxDiagnosticChars || contract.TTFTBasis != domain.TTFTBasisVisibleContent || contract.UserAgent != "Dyno" || contract.Telemetry != "strip X-Stainless-* at transport boundary" {
		t.Fatal("manifest config policy differs from adapter")
	}
	if contract.MinimumGo != "1.25.0" || contract.API != "chat/completions" || contract.Terminal != "choice zero finish (stop or length), exact framed [DONE], no premature EOF" || contract.UsageRequired != "final choices-empty usage chunk after finish; nonnegative integer prompt_tokens and completion_tokens; validate total_tokens if present" {
		t.Fatal("SDK/stream contract changed without validation")
	}
}
