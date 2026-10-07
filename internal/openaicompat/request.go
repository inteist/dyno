package openaicompat

import (
	"errors"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

func (c *Client) request(req llm.ChatRequest) (openai.ChatCompletionNewParams, error) {
	if err := c.ValidateOptions(req.Options, nil); err != nil {
		return openai.ChatCompletionNewParams{}, err
	}
	if req.Model.Name == "" || len(req.Conversation) == 0 {
		return openai.ChatCompletionNewParams{}, errors.New("openai chat: model ID and conversation are required")
	}
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Conversation))
	for _, m := range req.Conversation {
		switch m.Role {
		case domain.RoleSystem:
			messages = append(messages, openai.SystemMessage(m.Content))
		case domain.RoleUser:
			messages = append(messages, openai.UserMessage(m.Content))
		case domain.RoleAssistant:
			messages = append(messages, openai.AssistantMessage(m.Content))
		default:
			return openai.ChatCompletionNewParams{}, errors.New("openai chat: unsupported message role")
		}
	}
	p := openai.ChatCompletionNewParams{
		Model: shared.ChatModel(req.Model.Name), Messages: messages, N: openai.Int(1),
	}
	if c.config.TokenLimitField == "max_completion_tokens" {
		p.MaxCompletionTokens = openai.Int(int64(req.Options.MaxTokens))
	} else {
		p.MaxTokens = openai.Int(int64(req.Options.MaxTokens))
	}
	if !c.config.OmitSeed {
		p.Seed = openai.Int(int64(req.Options.Seed))
	}
	if !c.config.OmitTemperature {
		p.Temperature = openai.Float(req.Options.Temperature)
	}
	if c.config.Usage == "required" {
		p.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	}
	return p, nil
}
