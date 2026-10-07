package openaicompat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDiscoveryDefaultDeadlineAndEarlierCallerDeadline(t *testing.T) {
	for _, earlier := range []bool{false, true} {
		t.Run(fmt.Sprint(earlier), func(t *testing.T) {
			var calls atomic.Int32
			c := newTestClient(t, Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				deadline, ok := r.Context().Deadline()
				if !ok {
					t.Error("discovery has no deadline")
				}
				remaining := time.Until(deadline)
				if earlier {
					if remaining > time.Second {
						t.Error("caller deadline lost")
					}
				} else if remaining < 29*time.Second || remaining > 30*time.Second {
					t.Error("default discovery timeout is not 30s", remaining)
				}
				return nil, context.Canceled
			})}})
			ctx := context.Background()
			if earlier {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			_, err := c.Models(ctx, "supplied")
			if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
				t.Fatal("cancellation hidden/retried", err)
			}
			if c.config.DiscoveryTimeout != defaultDiscoveryTimeout {
				t.Fatal("incorrect default")
			}
		})
	}
	if _, err := New(Config{DiscoveryTimeout: -time.Second}); err == nil {
		t.Fatal("negative timeout accepted")
	}
}

type signalReadBody struct {
	io.ReadCloser
	ready chan struct{}
	once  sync.Once
}

func (b *signalReadBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.ready) })
	return b.ReadCloser.Read(p)
}

func TestDiscoveryTimeoutAndCallerCancellationCloseBody(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		headers, cancel, callerDeadline bool
	}{
		{"waiting-for-headers", false, false, false},
		{"waiting-for-body", true, false, false},
		{"caller-cancel", true, true, false},
		{"caller-deadline", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reading, stopped := make(chan struct{}), make(chan struct{})
			var calls, closed atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if tc.headers {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"data":[`)
					w.(http.Flusher).Flush()
				}
				<-r.Context().Done()
				close(stopped)
			}))
			defer s.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 60 * time.Millisecond
			if tc.callerDeadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), timeout)
				defer cancel()
				timeout = time.Second
			}
			hc := trackedHTTPClient(&closed)
			if tc.cancel {
				next := hc.Transport
				hc.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					res, err := next.RoundTrip(r)
					if err == nil {
						res.Body = &signalReadBody{ReadCloser: res.Body, ready: reading}
					}
					return res, err
				})
				go func() { <-reading; cancel() }()
			}
			c := newTestClient(t, Config{BaseURL: s.URL, DiscoveryTimeout: timeout, HTTPClient: hc})
			start := time.Now()
			_, err := c.Models(ctx, "supplied")
			want := context.DeadlineExceeded
			if tc.cancel {
				want = context.Canceled
			}
			if !errors.Is(err, want) || calls.Load() != 1 || time.Since(start) > time.Second {
				t.Fatal("discovery deadline/cancellation/retry", err, calls.Load())
			}
			if tc.headers && closed.Load() != 1 {
				t.Fatal("model body not closed", closed.Load())
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("request left running")
			}
		})
	}
}

func TestDiscoveryTimeoutDoesNotLimitChatOrChangeHTTPTransport(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(80 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fullStream())
	}))
	defer s.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	hc := &http.Client{Transport: transport}
	c := newTestClient(t, Config{BaseURL: s.URL, DiscoveryTimeout: 10 * time.Millisecond, HTTPClient: hc})
	if _, err := c.Chat(context.Background(), request(), nil); err != nil {
		t.Fatal("discovery deadline applied to chat", err)
	}
	if transport.ResponseHeaderTimeout != 0 || hc.Timeout != 0 || hc.Transport != transport || calls.Load() != 1 {
		t.Fatal("caller transport/timeout changed or chat retried")
	}
}

func TestHTTPMetadataStrippedForChatAndModels(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-stainless-") {
				t.Error("SDK platform telemetry sent", name)
			}
		}
		if r.UserAgent() != "Dyno" {
			t.Error("non-generic User-Agent", r.UserAgent())
		}
		if strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"data":[{"id":"model"}]}`)
		} else {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, fullStream())
		}
	}))
	defer s.Close()
	c := newTestClient(t, Config{BaseURL: s.URL})
	if _, err := c.Models(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Chat(context.Background(), request(), nil); err != nil {
		t.Fatal(err)
	}
}
