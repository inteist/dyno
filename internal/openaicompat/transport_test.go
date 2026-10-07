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
)

func TestPlainAndOversizedHTTPErrorsRemainRecognizable(t *testing.T) {
	for _, code := range []int{401, 403, 404, 405, 429, 500} {
		for _, oversized := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/%t", code, oversized), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(code)
					text := "secret plain HTTP failure"
					if oversized {
						text = strings.Repeat(text, maxErrorBytes)
					}
					_, _ = io.WriteString(w, text)
				}))
				defer s.Close()
				c := newTestClient(t, Config{BaseURL: s.URL})
				_, err := c.Chat(context.Background(), request(), nil)
				var httpErr *HTTPError
				if !errors.As(err, &httpErr) || httpErr.StatusCode != code || strings.Contains(err.Error(), "secret") {
					t.Fatal("HTTP status lost or secret leaked", err)
				}
				models, err := c.Models(context.Background(), "supplied")
				if code == 404 || code == 405 {
					if err != nil || len(models) != 1 || models[0].Name != "supplied" {
						t.Fatal("missing-route fallback failed", models, err)
					}
				} else if !errors.As(err, &httpErr) || httpErr.StatusCode != code {
					t.Fatal("plain HTTP auth/server failure hidden", err)
				}
				if oversized && code != 404 && code != 405 && (!errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "response size limit exceeded")) {
					t.Fatal("actual guard failure hidden by HTTP status/fallback", err)
				}
			})
		}
	}
}

func TestTLSCustomPrefixAndEscapedPath(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/proxy%2Fprefix/api/chat/completions" {
			t.Errorf("escaped base path lost: %s", r.URL.EscapedPath())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fullStream())
	}))
	defer s.Close()
	c := newTestClient(t, Config{BaseURL: s.URL + "/proxy%2Fprefix/api", HTTPClient: s.Client()})
	if _, err := c.Chat(context.Background(), request(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestBoundedBodyExactLimitAndClose(t *testing.T) {
	for _, size := range []int{0, 8, 9} {
		var closed atomic.Int32
		body := &boundedBody{ReadCloser: trackedBody{ReadCloser: io.NopCloser(strings.NewReader(strings.Repeat("x", size))), closed: &closed}, remaining: 8}
		data, err := io.ReadAll(body)
		if size > 8 {
			if !errors.Is(err, ErrProtocol) || len(data) > 8 {
				t.Fatal("body exceeded bound", len(data), err)
			}
		} else if err != nil || len(data) != size {
			t.Fatal("exact-size body truncated", len(data), err)
		}
		_ = body.Close()
		_ = body.Close()
		if closed.Load() != 1 {
			t.Fatal("body closed more than once")
		}
	}
}

func TestGuardAggregateLimit(t *testing.T) {
	g := framingGuard{totalBytes: maxStreamBytes}
	g.observe([]byte(": comment\n\n"))
	if !errors.Is(g.err, ErrProtocol) {
		t.Fatal("unbounded aggregate stream")
	}
}

func TestModelBodiesBoundedAndMalformed(t *testing.T) {
	for _, payload := range []string{
		`{"data":null}`, `{"data":[{"id":null}]}`, `{"data":[{}]}`, `{broken}`,
		`{"data":[{"id":"` + strings.Repeat("x", maxModelsBytes) + `"}]}`,
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, payload)
		}))
		var closed atomic.Int32
		c := newTestClient(t, Config{BaseURL: s.URL, HTTPClient: trackedHTTPClient(&closed)})
		_, err := c.Models(context.Background(), "supplied")
		if !errors.Is(err, ErrProtocol) || closed.Load() != 1 {
			t.Fatal("invalid/unbounded/unclosed model response", err, closed.Load())
		}
		s.Close()
	}
}
