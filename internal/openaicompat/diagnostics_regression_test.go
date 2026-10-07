package openaicompat

import (
	"context"
	"crypto/tls"
	"dyno/internal/llm"
	"errors"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestDiagnosticRedactionIsIdempotentAndKeepsWordBoundaries(t *testing.T) {
	endpoint, _ := url.Parse("http://host/v1")
	short := newSanitizer("a", endpoint)
	if got := short("bad"); got != "b[redacted]d" || short(got) != got {
		t.Fatalf("re-redacted placeholder: %q", got)
	}
	for _, split := range []string{"sk-abcd\n1234", "sk-abcd\u202e1234", "sk-abcd\u20281234"} {
		got := newSanitizer("sk-abcd1234", endpoint)(split)
		if got != "[redacted]" {
			t.Fatalf("split key leaked: %q", got)
		}
	}
	if got := newSanitizer("", endpoint)("a\u2028b\u2029c"); got != "a b c" {
		t.Fatalf("unsafe separators: %q", got)
	}
	key := "<Key&Secret>"
	sanitize := newSanitizer(key, endpoint)
	got := sanitize("before\n\tafter " + strings.ToUpper(key) + " " + html.EscapeString(key))
	if !strings.Contains(got, "before  after") || strings.Contains(strings.ToLower(got), "secret") || sanitize(got) != got {
		t.Fatalf("unsafe/ambiguous redaction: %q", got)
	}
}

func TestDialTimeoutRetainsSafeOriginAndUnavailableClass(t *testing.T) {
	c := newTestClient(t, Config{BaseURL: "https://host.test/v1", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: context.DeadlineExceeded}
	})}})
	_, err := c.Chat(context.Background(), request(), nil)
	if !errors.Is(err, llm.ErrUnavailable) || !strings.Contains(err.Error(), "https://host.test") || !strings.Contains(err.Error(), "network timeout") {
		t.Fatal(err)
	}
}

func TestTLSHandshakeEOFRemainsUnavailable(t *testing.T) {
	c := newTestClient(t, Config{BaseURL: "https://host.test/v1", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		trace := httptrace.ContextClientTrace(r.Context())
		trace.TLSHandshakeDone(tls.ConnectionState{}, io.EOF)
		return nil, io.EOF
	})}})
	_, err := c.Chat(context.Background(), request(), nil)
	if !errors.Is(err, llm.ErrUnavailable) || !strings.Contains(err.Error(), "https://host.test") || !strings.Contains(err.Error(), "EOF") {
		t.Fatal(err)
	}
}

func TestMissingModelMessagesDoNotConfuseRoutes(t *testing.T) {
	for _, tc := range []struct {
		message string
		missing bool
	}{{`model "x" not found, try pulling it first`, true}, {`The model endpoint /v1/chat does not exist`, false}, {`model "openrouter/foo" not found`, true}, {`model "endpoint-model" does not exist`, true}} {
		state := &responseState{status: 404}
		extractHTTPDiagnostic([]byte(`{"error":{"message":`+strconv.Quote(tc.message)+`}}`), state, func(s string) string { return s })
		if state.modelMissing != tc.missing {
			t.Fatalf("message %q missing=%t", tc.message, state.modelMissing)
		}
	}
}

func TestShortCredentialsDoNotRejectValidCompatiblePath(t *testing.T) {
	for _, key := range []string{"1", "v", "v1", "/"} {
		_, err := New(Config{BaseURL: "http://host/v1", APIKey: key, HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, io.EOF })}})
		if err != nil {
			t.Fatal(key, err)
		}
	}
}
