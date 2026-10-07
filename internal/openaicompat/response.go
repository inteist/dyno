package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/respjson"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// Chat measures one inference request, with no discovery, retry, or negotiation
// inside the timed interval. Progress is content chunks, never inferred tokens.
func (c *Client) Chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	p, err := c.request(req)
	if err != nil {
		return llm.ChatResult{}, err
	}
	ctx, state := withResponseState(ctx)
	ctx, guard := withStreamGuard(ctx)
	s := chatStream{start: c.now(), progress: progress}
	decodedEvents := 0
	stream := c.chat.NewStreaming(ctx, p)
	defer stream.Close()
	for stream.Next() {
		now := c.now() // timestamp before processing the decoded content
		decodedEvents++
		if guard.err != nil {
			return llm.ChatResult{}, fmt.Errorf("openai chat: %w", guard.err)
		}
		if err := s.add(now, stream.Current(), c.config.Usage == "required"); err != nil {
			return llm.ChatResult{}, fmt.Errorf("openai chat: %w", err)
		}
	}
	end := c.now()
	if err := ctx.Err(); err != nil {
		return llm.ChatResult{}, err
	}
	if guard.err != nil {
		return llm.ChatResult{}, fmt.Errorf("openai chat: %w", guard.err)
	}
	if err := stream.Err(); err != nil {
		return llm.ChatResult{}, fmt.Errorf("openai chat: %w", mapError(err, true, state))
	}
	// The SDK accepts any [DONE] prefix. Match the exact event it consumed, not
	// a later marker that the HTTP reader may already have read ahead.
	if !guard.done || guard.terminalEvent != decodedEvents+1 {
		return llm.ChatResult{}, fmt.Errorf("openai chat: %w", protocolError("stream ended without terminal [DONE]"))
	}
	result, err := s.result(end, c.config.Usage == "required")
	if err != nil {
		return result, fmt.Errorf("openai chat: %w", err)
	}
	return result, nil
}

type chatStream struct {
	start    time.Time
	progress llm.ProgressFunc
	ttft     time.Duration
	chunks   int
	reply    strings.Builder
	finish   string
	usage    *domain.ServerMetrics
}

func (s *chatStream) add(now time.Time, chunk openai.ChatCompletionChunk, requireUsage bool) error {
	if !chunk.JSON.Choices.Valid() {
		return protocolError("missing or invalid choices")
	}
	if s.usage != nil {
		return protocolError("chunk after final usage")
	}
	if len(chunk.Choices) > 1 {
		return protocolError("multiple choices for n=1")
	}
	for _, choice := range chunk.Choices {
		index, err := tokenCount(choice.JSON.Index, choice.Index)
		if err != nil || index != 0 || !choice.JSON.Delta.Valid() {
			return protocolError("invalid choice index or delta")
		}
		if s.finish != "" {
			return protocolError("choice after finish reason")
		}
		if !stringField(choice.Delta.JSON.Content, choice.Delta.Content, false) || !stringField(choice.JSON.FinishReason, string(choice.FinishReason), false) {
			return protocolError("invalid content or finish reason")
		}
		if !stringField(choice.Delta.JSON.Role, string(choice.Delta.Role), false) || (choice.Delta.Role != "" && choice.Delta.Role != "assistant") {
			return protocolError("invalid assistant role")
		}
		if len(choice.Delta.ToolCalls) != 0 || choice.Delta.JSON.FunctionCall.Valid() {
			return protocolError("tool/function output is unsupported")
		}
		if content := choice.Delta.Content; content != "" {
			if s.reply.Len()+len(content) > maxReplyBytes {
				return protocolError("reply size limit exceeded")
			}
			if s.chunks == 0 {
				s.ttft = now.Sub(s.start)
			}
			s.chunks++
			s.reply.WriteString(content)
			if s.progress != nil {
				s.progress(s.chunks)
			}
		}
		if reason := choice.FinishReason; reason != "" {
			switch reason {
			case "stop", domain.DoneLength:
				s.finish = reason
			case "content_filter":
				return protocolError("completion was content-filtered")
			case "tool_calls", "function_call":
				return protocolError("tool/function completion is unsupported")
			default:
				return protocolError("unsupported finish reason")
			}
		}
	}
	if invalidField(chunk.JSON.Usage) {
		return protocolError("invalid usage object")
	}
	if chunk.JSON.Usage.Valid() && requireUsage {
		// Only final authoritative usage is accepted, not intermediate counts.
		if s.finish == "" || len(chunk.Choices) != 0 {
			return protocolError("usage must be a final chunk after finish")
		}
		metrics, err := usageMetrics(chunk.Usage)
		if err != nil {
			return err
		}
		s.usage = &metrics
	}
	return nil
}

func (s *chatStream) result(end time.Time, requireUsage bool) (llm.ChatResult, error) {
	if s.finish == "" {
		return llm.ChatResult{}, protocolError("stream ended without finish reason")
	}
	if requireUsage && s.usage == nil {
		return llm.ChatResult{}, protocolError("stream ended without final usage")
	}
	metrics := domain.ServerMetrics{Available: &domain.MetricAvailability{}}
	if s.usage != nil {
		metrics = *s.usage
	}
	return llm.ChatResult{
		Reply: domain.Message{Role: domain.RoleAssistant, Content: s.reply.String()},
		Measurement: domain.Measurement{
			Server: metrics, DoneReason: s.finish,
			Client: domain.ClientMetrics{TimeToFirstToken: s.ttft, Wall: end.Sub(s.start)},
		},
	}, nil
}

// Generated SDK decoders coerce numbers/bools into strings. Benchmark-visible
// text and opaque IDs must instead preserve the response's actual JSON type.
func stringField(field respjson.Field, value string, required bool) bool {
	raw := field.Raw()
	if raw == "" || raw == "null" {
		return !required && value == ""
	}
	var decoded string
	return field.Valid() && json.Unmarshal([]byte(raw), &decoded) == nil && decoded == value && (!required || value != "")
}

func invalidField(field respjson.Field) bool {
	return !field.Valid() && field.Raw() != "" && field.Raw() != "null"
}

// tokenCount also validates the literal integer: generated SDK decoders can be
// permissive about numeric conversions. Counts must be nonnegative Go ints.
func tokenCount(field respjson.Field, value int64) (int, error) {
	if !field.Valid() {
		return 0, protocolError("missing or invalid token count")
	}
	n, err := strconv.ParseInt(field.Raw(), 10, 64)
	if err != nil || n != value || n < 0 || uint64(n) > uint64(^uint(0)>>1) {
		return 0, protocolError("token count out of range")
	}
	return int(n), nil
}

func usageMetrics(u openai.CompletionUsage) (domain.ServerMetrics, error) {
	m := domain.ServerMetrics{Available: &domain.MetricAvailability{}}
	var err error
	m.PromptTokens, err = tokenCount(u.JSON.PromptTokens, u.PromptTokens)
	if err != nil {
		return m, err
	}
	m.EvalTokens, err = tokenCount(u.JSON.CompletionTokens, u.CompletionTokens)
	if err != nil {
		return m, err
	}
	m.Available.PromptTokens, m.Available.EvalTokens = true, true
	if invalidField(u.JSON.TotalTokens) {
		return m, protocolError("invalid total token count")
	}
	if u.JSON.TotalTokens.Valid() {
		total, err := tokenCount(u.JSON.TotalTokens, u.TotalTokens)
		if err != nil || total < m.PromptTokens || total-m.PromptTokens != m.EvalTokens {
			return m, protocolError("inconsistent total token count")
		}
	}
	if invalidField(u.JSON.PromptTokensDetails) || invalidField(u.PromptTokensDetails.JSON.CachedTokens) {
		return m, protocolError("invalid cached token details")
	}
	if u.JSON.PromptTokensDetails.Valid() && u.PromptTokensDetails.JSON.CachedTokens.Valid() {
		m.CachedTokens, err = tokenCount(u.PromptTokensDetails.JSON.CachedTokens, u.PromptTokensDetails.CachedTokens)
		if err != nil || m.CachedTokens > m.PromptTokens {
			return m, protocolError("cached token count out of range")
		}
		m.Available.CachedTokens = true
	}
	// Standard chat usage never supplies prompt/eval/load/total durations.
	return m, nil
}
