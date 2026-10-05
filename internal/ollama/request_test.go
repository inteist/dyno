package ollama

import (
	"context"
	"reflect"
	"testing"
	"time"

	"token-rate/internal/domain"
)

func TestChatRequestJSON(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		thinking bool
	}{
		{"thinking model", true},
		{"model without thinking", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t, map[string]route{"/api/chat": ok(fixture(t, "chat-lfm2.5-8b-warm.ndjson"))})
			req := chatRequest(domain.Model{Name: ggufModel, Thinking: tc.thinking})
			if _, err := srv.client(time.Now).Chat(context.Background(), req, nil); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"model": ggufModel,
				"messages": []any{
					map[string]any{"role": "system", "content": "Be brief."},
					map[string]any{"role": "user", "content": "[run K3F9A2QX] Count from one to twenty."},
				},
				"stream":     true,
				"keep_alive": "10m0s",
				"truncate":   false,
				"shift":      false,
				"options": map[string]any{
					"num_predict": 12.0,
					"num_ctx":     8192.0,
					"temperature": 0.5,
					"seed":        42.0,
				},
			}
			if tc.thinking {
				want["think"] = false
			}
			if got := srv.onlyRequest("/api/chat"); !reflect.DeepEqual(got, want) {
				t.Errorf("request =\n%v\nwant\n%v", got, want)
			}
		})
	}
}

func TestGenerateRequestJSON(t *testing.T) {
	t.Parallel()
	load := func(contextSize int) func(*Client) error {
		return func(c *Client) error {
			_, err := c.Load(context.Background(), ggufModel, 10*time.Minute, contextSize)
			return err
		}
	}
	unload := func(c *Client) error {
		return c.Unload(context.Background(), ggufModel)
	}
	tests := []struct {
		name      string
		call      func(*Client) error
		body      string
		keepAlive string
		options   any
	}{
		{"load", load(0), "generate-load-lfm2.5-8b.ndjson", "10m0s", nil},
		{"load with a context size", load(8192), "generate-load-lfm2.5-8b.ndjson", "10m0s", map[string]any{"num_ctx": 8192.0}},
		{"unload", unload, "generate-unload-lfm2.5-8b.ndjson", "0s", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t, map[string]route{"/api/generate": ok(fixture(t, tc.body))})
			if err := tc.call(srv.client(time.Now)); err != nil {
				t.Fatal(err)
			}
			got := srv.onlyRequest("/api/generate")
			want := map[string]any{
				"model":      ggufModel,
				"prompt":     "",
				"keep_alive": tc.keepAlive,
				"shift":      false,
				"options":    tc.options,
			}
			for key, value := range want {
				if !reflect.DeepEqual(got[key], value) {
					t.Errorf("request %s = %v, want %v", key, got[key], value)
				}
			}
			if _, found := got["think"]; found {
				t.Errorf("request has think: %v", got["think"])
			}
		})
	}
}
