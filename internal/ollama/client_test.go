package ollama

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"dyno/internal/domain"
)

func TestHostURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		host string
		want string
	}{
		{"127.0.0.1:11434", "http://127.0.0.1:11434"},
		{"localhost", "http://localhost:11434"},
		{"studio.local:8080", "http://studio.local:8080"},
		{" studio.local ", "http://studio.local:11434"},
		{"http://localhost:8080", "http://localhost:8080"},
		{"http://localhost", "http://localhost:80"},
		{"https://ollama.example.com", "https://ollama.example.com:443"},
		{"https://ollama.example.com:443", "https://ollama.example.com:443"},
		{":8080", "http://127.0.0.1:8080"},
		{"studio.local:", "http://studio.local:11434"},
		{"::1", "http://[::1]:11434"},
		{"[::1]", "http://[::1]:11434"},
		{"[::1]:9000", "http://[::1]:9000"},
		{"http://proxy:80/ollama", "http://proxy:80/ollama"},
	}
	for _, tc := range tests {
		t.Run(tc.host, func(t *testing.T) {
			t.Parallel()
			got, err := hostURL(tc.host)
			if err != nil {
				t.Fatalf("hostURL(%q): %v", tc.host, err)
			}
			if got.String() != tc.want {
				t.Errorf("hostURL(%q) = %s, want %s", tc.host, got, tc.want)
			}
		})
	}
}

func TestNewRejectsBadHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"ftp://studio.local", "studio.local:port", "studio.local:70000", "http://studio.local:-1"} {
		if _, err := New(host); err == nil {
			t.Errorf("New(%q) has no error", host)
		}
	}
}

func TestNewReachesTheServerInEachHostForm(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{"/api/version": ok(fixture(t, "version.json"))})
	for _, host := range []string{srv.URL, srv.URL + "/", strings.TrimPrefix(srv.URL, "http://")} {
		checkVersion(t, host)
	}
}

// TestNewUsesOllamaHost cannot run in parallel, because it sets OLLAMA_HOST.
func TestNewUsesOllamaHost(t *testing.T) {
	srv := newFakeServer(t, map[string]route{"/api/version": ok(fixture(t, "version.json"))})
	t.Setenv("OLLAMA_HOST", strings.TrimPrefix(srv.URL, "http://"))
	checkVersion(t, "")
}

func checkVersion(t *testing.T, host string) {
	t.Helper()
	c, err := New(host)
	if err != nil {
		t.Fatalf("New(%q): %v", host, err)
	}
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("New(%q).Version: %v", host, err)
	}
	if v != "0.34.0" {
		t.Errorf("New(%q).Version = %q, want 0.34.0", host, v)
	}
}

func TestModelsShowsOnlyTheSelectedModels(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{
		"/api/tags":              ok(fixture(t, "tags.json")),
		"/api/show " + ggufModel: ok(fixture(t, "show-lfm2.5-8b.json")),
	})
	got, err := srv.client(time.Now).Models(context.Background(), ggufModel, "missing:tag")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != ggufModel {
		t.Errorf("Models() = %+v, want only %s", got, ggufModel)
	}
	if show := srv.onlyRequest("/api/show"); show["model"] != ggufModel {
		t.Errorf("show request = %v, want one for %s", show, ggufModel)
	}
}

func TestModelsTakesTheDetailsFromShow(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{
		"/api/tags":              ok(fixture(t, "tags.json")),
		"/api/show " + mlxModel:  ok(fixture(t, "show-qwen3.5-4b-mlx.json")),
		"/api/show " + ggufModel: ok(fixture(t, "show-lfm2.5-8b.json")),
	})
	got, err := srv.client(time.Now).Models(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Model{
		{
			Name:         mlxModel,
			Digest:       "61aa3858e9d3022e8fca725550089addd9289c4446f33bd09dfd12f95f2a6792",
			Format:       "safetensors",
			Family:       "qwen3_5", // empty in the tags list
			Parameters:   "4.5B",    // empty in the tags list
			Quantization: "nvfp4",
			SizeBytes:    3973305013,
			Thinking:     true,
		},
		{
			Name:         ggufModel,
			Digest:       "9cf756159fc2f3b9128c6a3f544ec90c5e9b8afdbb4179a57b8aea9de589cfb2",
			Format:       "gguf",
			Family:       "lfm2moe",
			Parameters:   "8.5B",
			Quantization: "Q4_K_M",
			SizeBytes:    5156075525,
			Thinking:     true,
		},
	}
	if !slices.Equal(got, want) {
		t.Errorf("Models =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRunningSpellsNamesAsTheTagsList(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{"/api/ps": ok(fixture(t, "ps.json"))})
	got, err := srv.client(time.Now).Running(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{mlxModel}; !slices.Equal(got, want) {
		t.Fatalf("Running = %v, want %v", got, want)
	}
	if tags := tagNames(t); !slices.Contains(tags, got[0]) {
		t.Errorf("Running name %q is not in the tags list %v", got[0], tags)
	}
}

// tagNames returns the model names of the tags fixture.
func tagNames(t *testing.T) []string {
	t.Helper()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(fixture(t, "tags.json"), &tags); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names
}

func TestLoad(t *testing.T) {
	t.Parallel()
	const step = 800 * time.Millisecond
	tests := []struct {
		name string
		body []byte
		want time.Duration
	}{
		{"recorded: no load_duration, so the request time", fixture(t, "generate-load-qwen3.5-4b-mlx.ndjson"), step},
		{"recorded GGUF", fixture(t, "generate-load-lfm2.5-8b.ndjson"), step},
		{"with load_duration", []byte(`{"model":"m","response":"","done":true,"done_reason":"load","load_duration":5000000}`), 5 * time.Millisecond},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := newFakeServer(t, map[string]route{"/api/generate": ok(tc.body)})
			got, err := srv.client(stepClock(step)).Load(context.Background(), mlxModel, 10*time.Minute, 8192)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Load = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUnload(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{"/api/generate": ok(fixture(t, "generate-unload-lfm2.5-8b.ndjson"))})
	if err := srv.client(time.Now).Unload(context.Background(), ggufModel); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateNeedsADoneChunk(t *testing.T) {
	t.Parallel()
	srv := newFakeServer(t, map[string]route{"/api/generate": ok(nil)})
	c := srv.client(time.Now)
	if _, err := c.Load(context.Background(), mlxModel, time.Minute, 8192); err == nil {
		t.Error("Load of an empty response has no error")
	}
	if err := c.Unload(context.Background(), mlxModel); err == nil {
		t.Error("Unload of an empty response has no error")
	}
}

func TestChatNeedsADoneChunk(t *testing.T) {
	t.Parallel()
	ls := lines(fixture(t, "chat-qwen3.5-4b-mlx-cold.ndjson"))
	srv := newFakeServer(t, map[string]route{"/api/chat": ok(joinLines(ls[:len(ls)-1]))})
	_, err := srv.client(time.Now).Chat(context.Background(), chatRequest(domain.Model{Name: mlxModel}), nil)
	if err == nil {
		t.Error("Chat of a stream without a done chunk has no error")
	}
}
