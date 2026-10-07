package ollama

import (
	"slices"
	"strings"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/types/model"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// chatStream collects the chunks of one streamed chat response.
type chatStream struct {
	start    time.Time
	progress llm.ProgressFunc
	ttft     time.Duration
	chunks   int
	reply    strings.Builder
	final    *api.ChatResponse
}

func newChatStream(start time.Time, progress llm.ProgressFunc) *chatStream {
	if progress == nil {
		progress = func(int) {}
	}
	return &chatStream{start: start, progress: progress}
}

// add takes the chunk r that arrived at now.
func (s *chatStream) add(now time.Time, r api.ChatResponse) {
	if hasContent(r.Message) {
		s.addContent(now, r.Message.Content)
	}
	if r.Done {
		s.final = &r
	}
}

func (s *chatStream) addContent(now time.Time, content string) {
	if s.chunks == 0 {
		s.ttft = now.Sub(s.start)
	}
	s.chunks++
	s.reply.WriteString(content)
	s.progress(s.chunks)
}

func hasContent(m api.Message) bool {
	return m.Content != "" || m.Thinking != ""
}

// result returns the reply and the measurement of a stream that ended at end.
func (s *chatStream) result(end time.Time) (llm.ChatResult, error) {
	if s.final == nil {
		return llm.ChatResult{}, errIncomplete()
	}
	return llm.ChatResult{
		Reply: domain.Message{Role: domain.RoleAssistant, Content: s.reply.String()},
		Measurement: domain.Measurement{
			Server:     toServerMetrics(s.final.Metrics),
			Client:     domain.ClientMetrics{TimeToFirstToken: s.ttft, Wall: end.Sub(s.start)},
			DoneReason: s.final.DoneReason,
		},
	}, nil
}

func toServerMetrics(m api.Metrics) domain.ServerMetrics {
	var availability *domain.MetricAvailability
	if m.PromptEvalCachedCount == nil {
		// Unlike scalar API fields, cached count is optional even on Ollama.
		availability = &domain.MetricAvailability{
			PromptTokens: true, PromptDuration: true, EvalTokens: true,
			EvalDuration: true, LoadDuration: true, TotalDuration: true,
		}
	}
	return domain.ServerMetrics{
		Available:      availability,
		PromptTokens:   m.PromptEvalCount,
		CachedTokens:   valueOrZero(m.PromptEvalCachedCount),
		PromptDuration: m.PromptEvalDuration,
		EvalTokens:     m.EvalCount,
		EvalDuration:   m.EvalDuration,
		LoadDuration:   m.LoadDuration,
		TotalDuration:  m.TotalDuration,
	}
}

func valueOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// toModel takes the identity of a model from List and the details from Show.
func toModel(m api.ListModelResponse, show *api.ShowResponse) domain.Model {
	return domain.Model{
		Name:         m.Name,
		Digest:       m.Digest,
		Format:       show.Details.Format,
		Family:       show.Details.Family,
		Parameters:   show.Details.ParameterSize,
		Quantization: show.Details.QuantizationLevel,
		SizeBytes:    m.Size,
		Thinking:     slices.Contains(show.Capabilities, model.CapabilityThinking),
	}
}

// runningNames returns the Name of each loaded model. The server spells it
// like the Name in the tags list.
func runningNames(ps *api.ProcessResponse) []string {
	names := make([]string, 0, len(ps.Models))
	for _, m := range ps.Models {
		names = append(names, m.Name)
	}
	return names
}

// loadDuration returns the load time that the server reports, or wall when
// the response has none. Ollama 0.34.0 reports none for a load request.
func loadDuration(r api.GenerateResponse, wall time.Duration) time.Duration {
	if r.LoadDuration > 0 {
		return r.LoadDuration
	}
	return wall
}
