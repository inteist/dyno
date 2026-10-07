package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"dyno/internal/domain"
)

func TestUsagePresenceAndRanges(t *testing.T) {
	for _, tc := range []struct {
		name, usage  string
		cached, fail bool
	}{
		{"zero", `{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0,"prompt_tokens_details":{"cached_tokens":0}}`, true, false},
		{"absent-cache", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}`, false, false},
		{"null-cache", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":null}}`, false, false},
		{"null-details", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":null}`, false, false},
		{"cached", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":7}}`, true, false},
		{"missing-prompt", `{"completion_tokens":2}`, false, true},
		{"null-prompt", `{"prompt_tokens":null,"completion_tokens":2}`, false, true},
		{"missing-completion", `{"prompt_tokens":10}`, false, true},
		{"null-completion", `{"prompt_tokens":10,"completion_tokens":null}`, false, true},
		{"negative", `{"prompt_tokens":10,"completion_tokens":-1}`, false, true},
		{"fraction", `{"prompt_tokens":1.5,"completion_tokens":2}`, false, true},
		{"string", `{"prompt_tokens":"10","completion_tokens":2}`, false, true},
		{"overflow", `{"prompt_tokens":9223372036854775808,"completion_tokens":2}`, false, true},
		{"cached-over-prompt", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":11}}`, false, true},
		{"negative-cache", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":-1}}`, false, true},
		{"invalid-cache", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":{"cached_tokens":"secret"}}`, false, true},
		{"invalid-details", `{"prompt_tokens":10,"completion_tokens":2,"prompt_tokens_details":"secret"}`, false, true},
		{"total-mismatch", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":13}`, false, true},
		{"invalid-total", `{"prompt_tokens":10,"completion_tokens":2,"total_tokens":"secret"}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := frame(contentChunk) + frame(finishChunk) + frame(`{"choices":[],"usage":`+tc.usage+`}`) + frame("[DONE]")
			s := streamServer(t, payload)
			c := newTestClient(t, Config{BaseURL: s.URL})
			res, err := c.Chat(context.Background(), request(), nil)
			if tc.fail {
				if !errors.Is(err, ErrProtocol) || strings.Contains(err.Error(), "secret") {
					t.Fatal("invalid usage accepted/leaked", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Measurement.Server.Has(domain.MetricCachedTokens) != tc.cached {
				t.Fatal("cache presence lost")
			}
			if !res.Measurement.Server.Has(domain.MetricPromptTokens) || !res.Measurement.Server.Has(domain.MetricEvalTokens) {
				t.Fatal("real counts unavailable")
			}
			if res.Measurement.Server.Has(domain.MetricEvalDuration) {
				t.Fatal("wall was used as server duration")
			}
		})
	}
}

func TestStrictStreamCompletion(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		usageOff      bool
	}{
		{"missing-DONE", frame(contentChunk) + frame(finishChunk) + frame(usageChunk), false},
		{"missing-finish", frame(contentChunk) + frame(usageChunk) + frame("[DONE]"), false},
		{"missing-usage", frame(contentChunk) + frame(finishChunk) + frame("[DONE]"), false},
		{"null-usage", frame(contentChunk) + frame(finishChunk) + frame(`{"choices":[],"usage":null}`) + frame("[DONE]"), false},
		{"premature-EOF", frame(contentChunk), false},
		{"unframed-DONE", frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + "data: [DONE]\n", false},
		{"DONE-prefix", frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + frame("[DONE]secret"), false},
		{"DONE-prefix-before-real-DONE", frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + frame("[DONE]secret") + frame("[DONE]"), false},
		{"DONE-multiline", frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + "data: [DONE]\ndata: extra\n\n", false},
		{"early-usage", frame(usageChunk) + frame(contentChunk) + frame(finishChunk) + frame("[DONE]"), false},
		{"nonfinal-usage", frame(contentChunk) + frame(finishChunk) + frame(usageChunk) + frame(roleChunk) + frame("[DONE]"), false},
		{"content-after-finish", frame(finishChunk) + frame(contentChunk) + frame(usageChunk) + frame("[DONE]"), false},
		{"invalid-JSON", frame(contentChunk) + frame("{broken secret}") + frame("[DONE]"), false},
		{"embedded-error", frame(contentChunk) + frame(`{"error":{"message":"secret"}}`) + frame("[DONE]"), false},
		{"error-event", frame(contentChunk) + "event: error\ndata: {\"message\":\"secret\"}\n\n" + frame("[DONE]"), false},
		{"wrong-choices", frame(`{"choices":null}`) + frame("[DONE]"), false},
		{"empty-object", frame(`{}`) + frame("[DONE]"), false},
		{"missing-index", frame(`{"choices":[{"delta":{"content":"hello"}}]}`) + frame("[DONE]"), false},
		{"wrong-index", frame(`{"choices":[{"index":1,"delta":{"content":"hello"}}]}`) + frame("[DONE]"), false},
		{"fraction-index", frame(`{"choices":[{"index":0.5,"delta":{"content":"hello"}}]}`) + frame("[DONE]"), false},
		{"wrong-content", frame(`{"choices":[{"index":0,"delta":{"content":12}}]}`) + frame("[DONE]"), false},
		{"unknown-finish", frame(`{"choices":[{"index":0,"delta":{},"finish_reason":"secret"}]}`) + frame(usageChunk) + frame("[DONE]"), false},
		{"tool-finish", frame(`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`) + frame(usageChunk) + frame("[DONE]"), false},
		{"filtered", frame(`{"choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`) + frame(usageChunk) + frame("[DONE]"), false},
		{"timing-only-missing-DONE", frame(contentChunk) + frame(finishChunk), true},
		{"timing-only-missing-finish", frame(contentChunk) + frame("[DONE]"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := streamServer(t, tc.payload)
			cfg := Config{BaseURL: s.URL}
			if tc.usageOff {
				cfg.Usage = "off"
			}
			c := newTestClient(t, cfg)
			_, err := c.Chat(context.Background(), request(), nil)
			if !errors.Is(err, ErrProtocol) || strings.Contains(err.Error(), "secret") {
				t.Fatalf("incomplete/malformed response accepted or leaked: %v", err)
			}
		})
	}
}

func TestSDKFramingSplitCRLFCommentsAndMultiline(t *testing.T) {
	// JSON spans data lines (the SDK joins them). Every byte, including each CRLF,
	// is flushed separately; other tests combine multiple events in one read.
	payload := ": comment\r\n\r\nevent: message\r\ndata: {\"choices\": [\r\ndata: {\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\r\n\r\n" +
		strings.ReplaceAll(frame(finishChunk)+frame(usageChunk)+frame("[DONE]"), "\n", "\r\n")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := range len(payload) {
			_, _ = io.WriteString(w, payload[i:i+1])
			w.(http.Flusher).Flush()
		}
	}))
	defer s.Close()
	c := newTestClient(t, Config{BaseURL: s.URL})
	res, err := c.Chat(context.Background(), request(), nil)
	if err != nil || res.Reply.Content != "hello" {
		t.Fatal("SDK framing failed", res, err)
	}
}

func TestFirstNonemptyContentTimingAndEmptyCompletion(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprint(empty), func(t *testing.T) {
			payload := frame(roleChunk) + frame(`{"choices":[{"index":0,"delta":{"content":""},"finish_reason":null}]}`)
			if !empty {
				payload += frame(contentChunk)
			}
			payload += frame(finishChunk) + frame(usageChunk) + frame("[DONE]")
			s := streamServer(t, payload)
			c := newTestClient(t, Config{BaseURL: s.URL})
			base := time.Now()
			tick := 0
			c.now = func() time.Time { v := base.Add(time.Duration(tick) * time.Second); tick++; return v }
			res, err := c.Chat(context.Background(), request(), nil)
			if err != nil {
				t.Fatal(err)
			}
			wantTTFT, wantWall := 3*time.Second, 6*time.Second
			if empty {
				wantTTFT, wantWall = 0, 5*time.Second
			}
			if res.Measurement.Client.TimeToFirstToken != wantTTFT || res.Measurement.Client.Wall != wantWall {
				t.Fatalf("timing: %#v", res.Measurement.Client)
			}
		})
	}
}

func TestTimingOnlyWithoutUsageAndStopReason(t *testing.T) {
	payload := frame(contentChunk) + frame(strings.ReplaceAll(finishChunk, `"length"`, `"stop"`)) + frame("[DONE]")
	s := streamServer(t, payload)
	c := newTestClient(t, Config{BaseURL: s.URL, Usage: "off"})
	res, err := c.Chat(context.Background(), request(), nil)
	if err != nil || res.Measurement.DoneReason != "stop" || res.Measurement.Server.Available.PromptTokens {
		t.Fatal(res, err)
	}
}

type trackedBody struct {
	io.ReadCloser
	closed *atomic.Int32
}

func (b trackedBody) Close() error { b.closed.Add(1); return b.ReadCloser.Close() }

func trackedHTTPClient(closed *atomic.Int32) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		res, err := http.DefaultTransport.RoundTrip(r)
		if err == nil {
			res.Body = trackedBody{ReadCloser: res.Body, closed: closed}
		}
		return res, err
	})}
}

func TestResponseBodiesClosed(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		status        int
	}{
		{"complete", fullStream(), 200}, {"incomplete", frame(contentChunk), 200},
		{"invalid", frame(`{"error":{"message":"secret"}}`), 200}, {"HTTP-error", `{"error":{"message":"secret"}}`, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer s.Close()
			var closed atomic.Int32
			c := newTestClient(t, Config{BaseURL: s.URL, HTTPClient: trackedHTTPClient(&closed)})
			_, _ = c.Chat(context.Background(), request(), nil)
			if closed.Load() != 1 {
				t.Fatalf("body closed %d times", closed.Load())
			}
		})
	}
}

func TestStreamCancellationAndDeadline(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint(deadline), func(t *testing.T) {
			serverClosed := make(chan struct{})
			var posts atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, frame(contentChunk))
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(serverClosed)
			}))
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			want := context.Canceled
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
				want = context.DeadlineExceeded
			}
			defer cancel()
			var closed atomic.Int32
			c := newTestClient(t, Config{BaseURL: s.URL, HTTPClient: trackedHTTPClient(&closed)})
			progress := func(int) {
				if !deadline {
					cancel()
				}
			}
			_, err := c.Chat(ctx, request(), progress)
			if !errors.Is(err, want) || posts.Load() != 1 || closed.Load() != 1 {
				t.Fatal("cancellation/retry/close", err, posts.Load(), closed.Load())
			}
			select {
			case <-serverClosed:
			case <-time.After(time.Second):
				t.Fatal("stream left open")
			}
		})
	}
}

func TestBoundedResponseProcessing(t *testing.T) {
	for _, tc := range []struct {
		name, payload, contentType string
		status                     int
	}{
		{"oversized-line", "data: " + strings.Repeat("x", maxEventBytes+1) + "\n\n", "text/event-stream", 200},
		{"oversized-multiline-event", strings.Repeat("data: {}\n", maxEventBytes/9+1) + "\n", "text/event-stream", 200},
		{"oversized-error", strings.Repeat("secret", maxErrorBytes), "application/json", 500},
		{"wrong-content-type", fullStream(), "application/json", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.payload)
			}))
			defer s.Close()
			var closed atomic.Int32
			c := newTestClient(t, Config{BaseURL: s.URL, HTTPClient: trackedHTTPClient(&closed)})
			_, err := c.Chat(context.Background(), request(), nil)
			if err == nil || strings.Contains(err.Error(), "secret") || closed.Load() != 1 {
				t.Fatal("unbounded/unsafe/unclosed response", err, closed.Load())
			}
		})
	}
}
