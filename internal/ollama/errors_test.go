package ollama

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/ollama/ollama/api"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// method calls one method of the client.
type method struct {
	name     string
	useModel bool // a server can answer that the model is missing
	call     func(context.Context, *Client) error
}

func methods() []method {
	return []method{
		{"Version", false, func(ctx context.Context, c *Client) error {
			_, err := c.Version(ctx)
			return err
		}},
		{"Models", true, func(ctx context.Context, c *Client) error {
			_, err := c.Models(ctx)
			return err
		}},
		{"Running", false, func(ctx context.Context, c *Client) error {
			_, err := c.Running(ctx)
			return err
		}},
		{"Load", true, func(ctx context.Context, c *Client) error {
			_, err := c.Load(ctx, mlxModel, time.Minute, 8192)
			return err
		}},
		{"Unload", true, func(ctx context.Context, c *Client) error {
			return c.Unload(ctx, mlxModel)
		}},
		{"Chat", true, func(ctx context.Context, c *Client) error {
			_, err := c.Chat(ctx, chatRequest(domain.Model{Name: mlxModel}), nil)
			return err
		}},
	}
}

func TestErrorsOfAClosedServerWrapErrUnavailable(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	c := clientFor(t, srv, time.Now)
	srv.Close()
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			if err := m.call(context.Background(), c); !errors.Is(err, llm.ErrUnavailable) {
				t.Errorf("%s error = %v, want llm.ErrUnavailable", m.name, err)
			}
		})
	}
}

func TestErrorsOfAMissingModelWrapErrModelNotFound(t *testing.T) {
	t.Parallel()
	notFound := route{status: http.StatusNotFound, body: fixture(t, "chat-not-found.json")}
	srv := newFakeServer(t, map[string]route{
		"/api/tags":     ok(fixture(t, "tags.json")),
		"/api/show":     notFound,
		"/api/generate": notFound,
		"/api/chat":     notFound,
	})
	c := srv.client(time.Now)
	for _, m := range methods() {
		if !m.useModel {
			continue
		}
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			err := m.call(context.Background(), c)
			if !errors.Is(err, llm.ErrModelNotFound) {
				t.Fatalf("%s error = %v, want llm.ErrModelNotFound", m.name, err)
			}
			var status api.StatusError
			if !errors.As(err, &status) || status.ErrorMessage != "model 'dyno-missing:0b' not found" {
				t.Errorf("%s error = %v, want the server message", m.name, err)
			}
		})
	}
}

func TestErrorsOfACanceledContextStayContextErrors(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, nil)
	c := srv.client(time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, m := range methods() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			err := m.call(ctx, c)
			if !errors.Is(err, context.Canceled) || errors.Is(err, llm.ErrUnavailable) {
				t.Errorf("%s error = %v, want only context.Canceled", m.name, err)
			}
		})
	}
}

func TestChatCanceledDuringTheStream(t *testing.T) {
	t.Parallel()
	first := lines(fixture(t, "chat-qwen3.5-4b-mlx-cold.ndjson"))[0]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(append(first, '\n'))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // the stream stays open until the client goes away
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	progress := func(int) { cancel() }
	_, err := clientFor(t, srv, time.Now).Chat(ctx, chatRequest(domain.Model{Name: mlxModel}), progress)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Chat error = %v, want context.Canceled", err)
	}
}

func TestChatErrorsKeepOtherServerErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rt   route
	}{
		{"status 500", route{status: http.StatusInternalServerError, body: []byte(`{"error":"out of memory"}`)}},
		{"status 400 without JSON", route{status: http.StatusBadRequest, body: []byte("bad request")}},
		{"error chunk in the stream", ok([]byte(`{"error":"the prompt is too long"}` + "\n"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t, map[string]route{"/api/chat": tc.rt})
			_, err := srv.client(time.Now).Chat(context.Background(), chatRequest(domain.Model{Name: mlxModel}), nil)
			if err == nil || errors.Is(err, llm.ErrModelNotFound) || errors.Is(err, llm.ErrUnavailable) {
				t.Errorf("Chat error = %v, want an unmapped error", err)
			}
		})
	}
}

func TestMapError(t *testing.T) {
	t.Parallel()
	refused := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	other := errors.New("other")
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"refused dial", fmt.Errorf("post: %w", refused), llm.ErrUnavailable},
		{"bare ECONNREFUSED", syscall.ECONNREFUSED, llm.ErrUnavailable},
		{"other dial error", &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host"}}, llm.ErrUnavailable},
		{"status 404", api.StatusError{StatusCode: http.StatusNotFound, ErrorMessage: "model 'x' not found"}, llm.ErrModelNotFound},
		{"canceled", fmt.Errorf("post: %w", context.Canceled), context.Canceled},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
		{"status 500", api.StatusError{StatusCode: http.StatusInternalServerError}, nil},
		{"read error", &net.OpError{Op: "read", Err: other}, nil},
		{"other", other, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapError(tc.err)
			if !errors.Is(got, tc.err) {
				t.Errorf("mapError(%v) = %v, which does not wrap the error", tc.err, got)
			}
			if tc.want != nil && !errors.Is(got, tc.want) {
				t.Errorf("mapError(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if tc.want == nil && got != tc.err {
				t.Errorf("mapError(%v) = %v, want it unchanged", tc.err, got)
			}
		})
	}
}
