package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ollama/ollama/api"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// Names of the recorded models.
const (
	mlxModel  = "qwen3.5:4b-mlx"
	ggufModel = "lfm2.5:8b"
)

// epoch is the start of every test clock.
func epoch() time.Time {
	return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
}

// stepClock returns a clock that starts at epoch and moves by step on each
// call after the first.
func stepClock(step time.Duration) func() time.Time {
	var calls atomic.Int64
	return func() time.Time {
		return epoch().Add(time.Duration(calls.Add(1)-1) * step)
	}
}

// fixture returns a file of testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// lines returns the lines of an NDJSON stream.
func lines(stream []byte) [][]byte {
	return bytes.Split(bytes.TrimSpace(stream), []byte("\n"))
}

// joinLines returns an NDJSON stream of ls.
func joinLines(ls [][]byte) []byte {
	return append(bytes.Join(ls, []byte("\n")), '\n')
}

// route is what the fake server answers.
type route struct {
	status int
	body   []byte
}

func ok(body []byte) route {
	return route{status: http.StatusOK, body: body}
}

// fakeServer answers each request with a route and records the request
// bodies. A route key is a path, or a path and a model name, such as
// "/api/show lfm2.5:8b".
type fakeServer struct {
	*httptest.Server
	t      *testing.T
	routes map[string]route
	mu     sync.Mutex
	bodies map[string][][]byte
}

func newFakeServer(t *testing.T, routes map[string]route) *fakeServer {
	t.Helper()
	s := &fakeServer{t: t, routes: routes, bodies: map[string][][]byte{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *fakeServer) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.record(r.URL.Path, body)
	rt, found := s.route(r.URL.Path, body)
	if !found {
		s.t.Errorf("no route for %s %s", r.URL.Path, body)
		http.Error(w, "no route", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(rt.status)
	_, _ = w.Write(rt.body)
}

func (s *fakeServer) record(path string, body []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bodies[path] = append(s.bodies[path], body)
}

func (s *fakeServer) route(path string, body []byte) (route, bool) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &req) // a GET request has no body
	if rt, found := s.routes[path+" "+req.Model]; found {
		return rt, true
	}
	rt, found := s.routes[path]
	return rt, found
}

// requests returns the decoded bodies of the requests to path.
func (s *fakeServer) requests(path string) []map[string]any {
	s.t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs := make([]map[string]any, 0, len(s.bodies[path]))
	for _, b := range s.bodies[path] {
		var req map[string]any
		if err := json.Unmarshal(b, &req); err != nil {
			s.t.Fatalf("request to %s: %v", path, err)
		}
		reqs = append(reqs, req)
	}
	return reqs
}

// onlyRequest returns the body of the one request to path.
func (s *fakeServer) onlyRequest(path string) map[string]any {
	s.t.Helper()
	reqs := s.requests(path)
	if len(reqs) != 1 {
		s.t.Fatalf("%d requests to %s, want 1", len(reqs), path)
	}
	return reqs[0]
}

func (s *fakeServer) client(now func() time.Time) *Client {
	return clientFor(s.t, s.Server, now)
}

// clientFor returns a client of srv with the clock now.
func clientFor(t *testing.T, srv *httptest.Server, now func() time.Time) *Client {
	t.Helper()
	base, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return newClient(api.NewClient(base, srv.Client()), now)
}

// chatRequest returns a request for model with the options of the fixtures.
func chatRequest(model domain.Model) llm.ChatRequest {
	return llm.ChatRequest{
		Model: model,
		Conversation: domain.Conversation{
			{Role: domain.RoleSystem, Content: "Be brief."},
			{Role: domain.RoleUser, Content: "[run K3F9A2QX] Count from one to twenty."},
		},
		Options:   llm.GenerationOptions{MaxTokens: 12, ContextSize: 8192, Temperature: 0.5, Seed: 42},
		KeepAlive: 10 * time.Minute,
	}
}

// replayChat sends one chat to a server that answers with stream.
func replayChat(t *testing.T, stream []byte, now func() time.Time, progress llm.ProgressFunc) (llm.ChatResult, *fakeServer) {
	t.Helper()
	srv := newFakeServer(t, map[string]route{"/api/chat": ok(stream)})
	res, err := srv.client(now).Chat(context.Background(), chatRequest(domain.Model{Name: mlxModel}), progress)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	return res, srv
}
