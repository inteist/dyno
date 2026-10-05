package ollama

import (
	"time"

	"github.com/ollama/ollama/api"

	"token-rate/internal/domain"
	"token-rate/internal/llm"
)

// toChatRequest builds the streamed request with the options of plan section
// 10.2.
func toChatRequest(req llm.ChatRequest) *api.ChatRequest {
	r := &api.ChatRequest{
		Model:     req.Model.Name,
		Messages:  toMessages(req.Conversation),
		Stream:    new(true),
		KeepAlive: &api.Duration{Duration: req.KeepAlive},
		Truncate:  new(false), // fail instead of a cut prompt
		Shift:     new(false), // fail instead of a shifted context
		Options:   toOptions(req.Options),
	}
	if req.Model.Thinking {
		r.Think = &api.ThinkValue{Value: false}
	}
	return r
}

func toMessages(c domain.Conversation) []api.Message {
	msgs := make([]api.Message, 0, len(c))
	for _, m := range c {
		msgs = append(msgs, api.Message{Role: string(m.Role), Content: m.Content})
	}
	return msgs
}

func toOptions(o llm.GenerationOptions) map[string]any {
	return map[string]any{
		"num_predict": o.MaxTokens,
		"num_ctx":     o.ContextSize,
		"temperature": o.Temperature,
		"seed":        o.Seed,
	}
}

// toGenerateRequest builds a request with an empty prompt, which loads or
// unloads model. It sends the shift value and the context size of the chats,
// because the GGUF engine reloads a model when they differ from the load.
func toGenerateRequest(model string, keepAlive time.Duration, contextSize int) *api.GenerateRequest {
	r := &api.GenerateRequest{
		Model:     model,
		KeepAlive: &api.Duration{Duration: keepAlive},
		Shift:     new(false),
	}
	if contextSize > 0 {
		r.Options = map[string]any{"num_ctx": contextSize}
	}
	return r
}
