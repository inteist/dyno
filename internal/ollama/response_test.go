package ollama

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ollama/ollama/api"

	"dyno/internal/domain"
)

// recordedStream is one chat stream of testdata.
type recordedStream struct {
	file string
	warm bool // the second of two equal requests: the prompt comes from the cache
}

func recordedStreams() []recordedStream {
	return []recordedStream{
		{file: "chat-qwen3.5-4b-mlx-cold.ndjson"},
		{file: "chat-qwen3.5-4b-mlx-warm.ndjson", warm: true},
		{file: "chat-lfm2.5-8b-cold.ndjson"},
		{file: "chat-lfm2.5-8b-warm.ndjson", warm: true},
	}
}

// rawMetrics reads the metrics of a done chunk by their JSON names, without
// the api types.
func rawMetrics(t *testing.T, done []byte) domain.ServerMetrics {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(done, &raw); err != nil {
		t.Fatal(err)
	}
	n := func(key string) int64 {
		var v int64
		if b, found := raw[key]; found {
			if err := json.Unmarshal(b, &v); err != nil {
				t.Fatalf("%s: %v", key, err)
			}
		}
		return v
	}
	return domain.ServerMetrics{
		PromptTokens:   int(n("prompt_eval_count")),
		CachedTokens:   int(n("prompt_eval_cached_count")),
		PromptDuration: time.Duration(n("prompt_eval_duration")),
		EvalTokens:     int(n("eval_count")),
		EvalDuration:   time.Duration(n("eval_duration")),
		LoadDuration:   time.Duration(n("load_duration")),
		TotalDuration:  time.Duration(n("total_duration")),
	}
}

// contents returns the content of each chunk with content or thinking.
func contents(t *testing.T, stream []byte) []string {
	t.Helper()
	var out []string
	for _, line := range lines(stream) {
		var chunk struct {
			Message struct {
				Content  string `json:"content"`
				Thinking string `json:"thinking"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &chunk); err != nil {
			t.Fatal(err)
		}
		if chunk.Message.Content != "" || chunk.Message.Thinking != "" {
			out = append(out, chunk.Message.Content)
		}
	}
	return out
}

func TestChatMapsMetricsFieldByField(t *testing.T) {
	t.Parallel()
	cachedCountField := regexp.MustCompile(`"prompt_eval_cached_count":\d+,`)
	for _, rec := range recordedStreams() {
		for _, dropCached := range []bool{false, true} {
			name := rec.file
			if dropCached {
				name += " without prompt_eval_cached_count"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				stream := fixture(t, rec.file)
				if dropCached {
					stream = cachedCountField.ReplaceAll(stream, nil)
				}
				lastLine := lines(stream)[len(lines(stream))-1]
				res, _ := replayChat(t, stream, time.Now, nil)

				got, want := res.Measurement.Server, rawMetrics(t, lastLine)
				if got != want {
					t.Errorf("server metrics = %+v, want %+v", got, want)
				}
				if res.Measurement.DoneReason != domain.DoneLength {
					t.Errorf("done reason = %q, want %q", res.Measurement.DoneReason, domain.DoneLength)
				}
				checkRecorded(t, got, rec.warm && !dropCached)
			})
		}
	}
}

// checkRecorded checks that the fixture has the values that the test needs.
func checkRecorded(t *testing.T, m domain.ServerMetrics, cached bool) {
	t.Helper()
	if m.PromptTokens == 0 || m.EvalTokens == 0 || m.PromptDuration == 0 ||
		m.EvalDuration == 0 || m.LoadDuration == 0 || m.TotalDuration == 0 {
		t.Errorf("the fixture lacks a metric: %+v", m)
	}
	if got := m.CachedTokens > 0; got != cached {
		t.Errorf("cached tokens = %d, want cached = %v", m.CachedTokens, cached)
	}
}

func TestChatMeasuresTTFTAtTheFirstContentChunk(t *testing.T) {
	t.Parallel()
	const step = 10 * time.Millisecond
	stream := fixture(t, "chat-qwen3.5-4b-mlx-warm.ndjson")
	empty := []byte(`{"model":"qwen3.5:4b-mlx","message":{"role":"assistant","content":""},"done":false}` + "\n")
	thinking := []byte(`{"model":"qwen3.5:4b-mlx","message":{"role":"assistant","content":"","thinking":"Hm."},"done":false}` + "\n")
	tests := []struct {
		name   string
		stream []byte
		ttft   time.Duration
	}{
		{"content in the first chunk", stream, step},
		{"two empty chunks first", slices.Concat(empty, empty, stream), 3 * step},
		{"thinking in the first chunk", slices.Concat(thinking, stream), step},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, _ := replayChat(t, tc.stream, stepClock(step), nil)
			client := res.Measurement.Client
			if client.TimeToFirstToken != tc.ttft {
				t.Errorf("TTFT = %v, want %v", client.TimeToFirstToken, tc.ttft)
			}
			// One clock call at the start, one for each chunk, one at the end.
			if want := time.Duration(len(lines(tc.stream))+1) * step; client.Wall != want {
				t.Errorf("wall = %v, want %v", client.Wall, want)
			}
		})
	}
}

func TestChatReportsProgressForEachContentChunk(t *testing.T) {
	t.Parallel()
	for _, rec := range recordedStreams() {
		t.Run(rec.file, func(t *testing.T) {
			t.Parallel()
			stream := fixture(t, rec.file)
			var calls []int
			res, _ := replayChat(t, stream, time.Now, func(tokens int) { calls = append(calls, tokens) })

			chunks := contents(t, stream)
			if want := countTo(len(chunks)); !slices.Equal(calls, want) {
				t.Errorf("progress calls = %v, want %v", calls, want)
			}
			want := domain.Message{Role: domain.RoleAssistant, Content: strings.Join(chunks, "")}
			if res.Reply != want {
				t.Errorf("reply = %+v, want %+v", res.Reply, want)
			}
		})
	}
}

// countTo returns 1, 2, ..., n.
func countTo(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i + 1
	}
	return out
}

func TestChatKeepsThinkingOutOfTheReply(t *testing.T) {
	t.Parallel()
	stream := []byte(`{"message":{"role":"assistant","content":"","thinking":"Hm."},"done":false}
{"message":{"role":"assistant","content":"Hi."},"done":true,"done_reason":"stop"}
`)
	var calls []int
	res, _ := replayChat(t, stream, time.Now, func(tokens int) { calls = append(calls, tokens) })
	if res.Reply.Content != "Hi." {
		t.Errorf("reply = %q, want %q", res.Reply.Content, "Hi.")
	}
	if !slices.Equal(calls, []int{1, 2}) {
		t.Errorf("progress calls = %v, want [1 2]", calls)
	}
	if res.Measurement.DoneReason != "stop" {
		t.Errorf("done reason = %q, want stop", res.Measurement.DoneReason)
	}
}

func TestToModel(t *testing.T) {
	t.Parallel()
	list := api.ListModelResponse{Name: "m:1b", Digest: "abc", Size: 42}
	tests := []struct {
		name         string
		capabilities string
		thinking     bool
	}{
		{"with thinking", `["completion","thinking"]`, true},
		{"without thinking", `["completion","tools"]`, false},
		{"without capabilities", `null`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var show api.ShowResponse
			body := `{"details":{"format":"gguf","family":"f","parameter_size":"1B","quantization_level":"Q8_0"},"capabilities":` + tc.capabilities + `}`
			if err := json.Unmarshal([]byte(body), &show); err != nil {
				t.Fatal(err)
			}
			want := domain.Model{
				Name: "m:1b", Digest: "abc", Format: "gguf", Family: "f",
				Parameters: "1B", Quantization: "Q8_0", SizeBytes: 42, Thinking: tc.thinking,
			}
			if got := toModel(list, &show); got != want {
				t.Errorf("toModel = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLoadDurationPrefersTheServerValue(t *testing.T) {
	t.Parallel()
	const wall = time.Second
	tests := []struct {
		name string
		r    api.GenerateResponse
		want time.Duration
	}{
		{"server value", api.GenerateResponse{Metrics: api.Metrics{LoadDuration: 3 * time.Millisecond}}, 3 * time.Millisecond},
		{"no server value", api.GenerateResponse{}, wall},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := loadDuration(tc.r, wall); got != tc.want {
				t.Errorf("loadDuration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFixturesHaveOneDoneChunkAtTheEnd(t *testing.T) {
	t.Parallel()
	for _, rec := range recordedStreams() {
		ls := lines(fixture(t, rec.file))
		for i, line := range ls {
			if done := bytes.Contains(line, []byte(`"done":true`)); done != (i == len(ls)-1) {
				t.Errorf("%s line %d: done = %v", rec.file, i+1, done)
			}
		}
	}
}
