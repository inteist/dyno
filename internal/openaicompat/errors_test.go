package openaicompat

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/openai/openai-go/v3"

	"dyno/internal/llm"
)

// Inspect every wrapped object, not just Error(): SDK errors and url/net errors
// can hide secrets in fields even when their top-level formatting looks safe.
func assertSafeError(t *testing.T, err error, secrets ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
	var sdk *openai.Error
	var network *net.OpError
	var urlErr *url.Error
	if errors.As(err, &sdk) || errors.As(err, &network) || errors.As(err, &urlErr) {
		t.Fatal("raw SDK/transport object retained")
	}
	for e := err; e != nil; e = errors.Unwrap(e) {
		text := fmt.Sprintf("%s %+v %#v", e, e, e)
		for _, secret := range secrets {
			if secret != "" && strings.Contains(text, secret) {
				t.Fatal("secret retained in error chain")
			}
		}
		if strings.ContainsFunc(e.Error(), unicode.IsControl) {
			t.Fatal("control character in diagnostic")
		}
	}
}

type opaqueTransportError struct{ secret string }

func (e *opaqueTransportError) Error() string { return "benign summary" }

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestTransportFailureCausesAndSecretSafety(t *testing.T) {
	for _, tc := range []struct {
		name, cause string
		err         error
	}{
		{"reset", "connection reset by peer", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
		{"refused", "connection refused", syscall.ECONNREFUSED},
		{"EOF-before-headers", "EOF", io.EOF},
		{"unexpected-EOF-before-headers", "unexpected EOF", io.ErrUnexpectedEOF},
		{"DNS", "DNS lookup failed", &net.DNSError{Err: "hidden-secret", Name: "hidden-secret"}},
		{"DNS-not-found", "no such host", &net.DNSError{Err: "hidden-secret", Name: "hidden-secret", IsNotFound: true}},
		{"unreachable", "network is unreachable", syscall.ENETUNREACH},
		{"expired-TLS", "certificate has expired", x509.CertificateInvalidError{Cert: &x509.Certificate{DNSNames: []string{"hidden-secret"}}, Reason: x509.Expired, Detail: "hidden-secret"}},
		{"unknown-dial", "network dial failed", &net.OpError{Op: "dial", Net: "tcp", Err: &opaqueTransportError{secret: "hidden-secret"}}},
		{"opaque", "HTTP transport failure", &opaqueTransportError{secret: "hidden-secret"}},
		{"wrapped-URL", "connection reset by peer", &url.Error{Op: "Get", URL: "https://user:password@host/secret-path?key=query-secret#fragment-secret", Err: syscall.ECONNRESET}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			c := newTestClient(t, Config{BaseURL: "https://example.test/secret-path", APIKey: "resolved-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return nil, tc.err
			})}})
			for _, chat := range []bool{true, false} {
				var err error
				if chat {
					_, err = c.Chat(context.Background(), request(), nil)
				} else {
					_, err = c.Models(context.Background(), "supplied")
				}
				wantUnavailable := tc.name != "reset" && tc.name != "EOF-before-headers" && tc.name != "unexpected-EOF-before-headers" && tc.name != "opaque" && tc.name != "wrapped-URL"
				if errors.Is(err, llm.ErrUnavailable) != wantUnavailable || errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), tc.cause) || !strings.Contains(err.Error(), "https://example.test") {
					t.Fatal("transport classification or cause lost", err)
				}
				assertSafeError(t, err, "hidden-secret", "resolved-secret", "secret-path", "password", "query-secret", "fragment-secret")
			}
			if calls.Load() != 2 {
				t.Fatal("retried transport failure", calls.Load())
			}
		})
	}
}

func TestRealTLSAndHTTPSAgainstHTTPFailures(t *testing.T) {
	for _, tlsServer := range []bool{true, false} {
		t.Run(fmt.Sprint(tlsServer), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, fullStream()) })
			var s *httptest.Server
			if tlsServer {
				s = httptest.NewTLSServer(handler)
			} else {
				s = httptest.NewServer(handler)
			}
			defer s.Close()
			base := strings.Replace(s.URL, "http://", "https://", 1) + "/secret-path"
			c := newTestClient(t, Config{BaseURL: base, APIKey: "resolved-secret"})
			_, err := c.Chat(context.Background(), request(), nil)
			want := "unknown authority"
			if !tlsServer {
				want = "HTTP response to HTTPS client"
			}
			if !errors.Is(err, llm.ErrUnavailable) || errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), want) {
				t.Fatal("TLS cause lost", err)
			}
			assertSafeError(t, err, "resolved-secret", "secret-path")
		})
	}
}

func TestStreamNetworkResetVersusTruncatedProtocol(t *testing.T) {
	for _, tc := range []struct {
		name        string
		readErr     error
		interrupted bool
		cause       string
	}{
		{"clean-EOF", io.EOF, false, "terminal [DONE]"},
		{"truncated-body", io.ErrUnexpectedEOF, false, "truncated response body"},
		{"reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}, true, "connection reset by peer"},
		{"network-EOF", &net.OpError{Op: "read", Net: "tcp", Err: io.EOF}, true, "EOF"},
		{"opaque-read-error", &opaqueTransportError{secret: "hidden-secret"}, true, "HTTP transport failure"},
		{"opaque-protocol", fmt.Errorf("%w: hidden-secret", ErrProtocol), false, "incompatible response"},
		{"wrapped-EOF", fmt.Errorf("hidden-secret: %w", io.EOF), false, "terminal [DONE]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls, closed atomic.Int32
			c := newTestClient(t, Config{BaseURL: "http://host.test/secret-path", HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				body := io.MultiReader(strings.NewReader(frame(contentChunk)+frame(finishChunk)+frame(usageChunk)), failingReader{tc.readErr})
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: trackedBody{io.NopCloser(body), &closed}, Request: r}, nil
			})}})
			_, err := c.Chat(context.Background(), request(), nil)
			if errors.Is(err, llm.ErrUnavailable) || errors.Is(err, ErrRequestInterrupted) != tc.interrupted || errors.Is(err, ErrProtocol) == tc.interrupted || !strings.Contains(err.Error(), tc.cause) {
				t.Fatal("wrong read-failure category", err)
			}
			assertSafeError(t, err, "secret-path", "hidden-secret")
			if calls.Load() != 1 || closed.Load() != 1 {
				t.Fatal("retry or leaked body")
			}
		})
	}
}

func TestRealResetAndEOFBeforeHTTPHeaders(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprint(reset), func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				if reset {
					_ = conn.(*net.TCPConn).SetLinger(0)
				}
				_ = conn.Close()
			}))
			defer s.Close()
			c := newTestClient(t, Config{BaseURL: s.URL + "/secret-path"})
			_, err := c.Chat(context.Background(), request(), nil)
			if errors.Is(err, llm.ErrUnavailable) || !errors.Is(err, ErrRequestInterrupted) || errors.Is(err, ErrProtocol) || calls.Load() != 1 {
				t.Fatal("network EOF/reset/retry", err, calls.Load())
			}
			assertSafeError(t, err, "secret-path")
		})
	}
}

func TestBoundedStructuredRequestFeedback(t *testing.T) {
	for _, flat := range []bool{false, true} {
		t.Run(fmt.Sprint(flat), func(t *testing.T) {
			key := "resolved+key"
			message := "stream_options not supported\x00\x7f\x1b\n: " + key + " https://user:password@HOST.test:8443/secret-path?q=query-secret#fragment-secret Authorization: Bearer reflected-key; " + strings.Repeat("界", 220)
			fields := map[string]any{"message": message, "type": "invalid_request_error", "code": "unsupported_parameter"}
			var body any = fields
			if !flat {
				body = map[string]any{"error": fields}
			}
			payload, _ := json.Marshal(body)
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Reflected-Key", key)
				w.WriteHeader(400)
				_, _ = w.Write(payload)
			}))
			defer s.Close()
			c := newTestClient(t, Config{BaseURL: s.URL, APIKey: key})
			_, err := c.Chat(context.Background(), request(), nil)
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != 400 || !strings.Contains(httpErr.Message, "stream_options not supported") || !strings.Contains(httpErr.Message, "https://host.test:8443") || len([]rune(httpErr.Message)) > maxDiagnosticChars || calls.Load() != 1 {
				t.Fatal("request feedback missing/unbounded/retried", err)
			}
			assertSafeError(t, err, key, "password", "secret-path", "query-secret", "fragment-secret", "reflected-key")
			if c.config.APIKey != "" || strings.Contains(c.info.ConfigJSON, key) {
				t.Fatal("credential copied into provenance")
			}
		})
	}
}

func TestFlatMissingModelAndStrictErrorFields(t *testing.T) {
	for _, tc := range []struct {
		body  string
		model bool
	}{
		{`{"message":"Model 'resolved-key' does not exist.","type":"NotFoundError","code":"model_not_found"}`, true},
		{`{"message":"Model 'resolved-key' does not exist.","type":"NotFoundError"}`, true},
		{`{"type":"model_not_found","message":"resolved-key"}`, true},
		{`{"message":"Not Found"}`, false},
		{`{"code":12,"message":true}`, false},
		{`{"error":{"message":"Not Found"},"code":"model_not_found"}`, false},
		{`{"error":"model_not_found"}`, false},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = io.WriteString(w, tc.body)
		}))
		c := newTestClient(t, Config{BaseURL: s.URL, APIKey: "resolved-key"})
		_, err := c.Chat(context.Background(), request(), nil)
		if errors.Is(err, llm.ErrModelNotFound) != tc.model {
			t.Fatal("flat error misclassified", err)
		}
		assertSafeError(t, err, "resolved-key")
		s.Close()
	}
}

func TestSDKErrorPayloadAndHeadersNotRetained(t *testing.T) {
	for _, payload := range []string{frame(`{"error":{"message":"resolved-key","headers":{"Authorization":"Bearer reflected-key"}}}`), frame(`{malformed resolved-key}`)} {
		s := streamServer(t, payload)
		c := newTestClient(t, Config{BaseURL: s.URL, APIKey: "resolved-key"})
		_, err := c.Chat(context.Background(), request(), nil)
		if !errors.Is(err, ErrProtocol) {
			t.Fatal(err)
		}
		assertSafeError(t, err, "resolved-key", "reflected-key")
	}
}

func TestAPIKeyControlsRejected(t *testing.T) {
	for _, control := range []rune{0, 1, 9, 10, 13, 31, 127, 128, 133, 159} {
		_, err := New(Config{APIKey: "key" + string(control) + "hidden-secret"})
		assertSafeError(t, err, "hidden-secret")
	}
}

func TestRealNetworkResetDuringSSE(t *testing.T) {
	received := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, buffered, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		chunk := frame(contentChunk)
		_, _ = fmt.Fprintf(buffered, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n", len(chunk), chunk)
		_ = buffered.Flush()
		select {
		case <-received:
		case <-time.After(time.Second):
			return
		}
		_ = conn.(*net.TCPConn).SetLinger(0)
	}))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c := newTestClient(t, Config{BaseURL: s.URL + "/secret-path", APIKey: "resolved-key"})
	_, err := c.Chat(ctx, request(), func(int) { close(received) })
	if errors.Is(err, llm.ErrUnavailable) || !errors.Is(err, ErrRequestInterrupted) || errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "connection reset by peer") {
		t.Fatal("real network reset classified as protocol", err)
	}
	assertSafeError(t, err, "secret-path", "resolved-key")
}

func TestDiscoveryReadFailureDoesNotBecomeMissingRouteFallback(t *testing.T) {
	for _, code := range []int{200, 404, 405} {
		var calls, closed atomic.Int32
		c := newTestClient(t, Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			calls.Add(1)
			body := io.MultiReader(strings.NewReader(`{"error":{"message":"resolved-key"}}`), failingReader{&net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}})
			return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": {"application/json"}}, Body: trackedBody{io.NopCloser(body), &closed}, Request: r}, nil
		})}})
		_, err := c.Models(context.Background(), "supplied")
		if errors.Is(err, llm.ErrUnavailable) || !errors.Is(err, ErrRequestInterrupted) || errors.Is(err, ErrDiscoveryUnsupported) || calls.Load() != 1 || closed.Load() != 1 {
			t.Fatal("read failure disguised as discovery fallback/retried/leaked", err)
		}
		assertSafeError(t, err, "resolved-key")
	}
}
