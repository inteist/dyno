// Package llm is the port between the core and an LLM server. The ollama
// package implements it, and llmtest gives a fake for tests.
package llm

import (
	"context"
	"errors"
	"time"

	"token-rate/internal/domain"
)

// GenerationOptions are the sampling and context settings of a request.
type GenerationOptions struct {
	MaxTokens   int // num_predict
	ContextSize int // num_ctx
	Temperature float64
	Seed        int
}

// ChatRequest is one chat request.
type ChatRequest struct {
	Model        domain.Model
	Conversation domain.Conversation
	Options      GenerationOptions
	KeepAlive    time.Duration
}

// ChatResult is the reply and the measurement of one chat request.
type ChatResult struct {
	Reply       domain.Message
	Measurement domain.Measurement
}

// ProgressFunc receives the number of streamed chunks so far. The count is
// approximate. The final count comes from the server metrics.
type ProgressFunc func(tokens int)

// Client is what the core needs from an LLM server.
type Client interface {
	// Version returns the server version.
	Version(ctx context.Context) (string, error)
	// Models returns the local models with their details. With names, it
	// returns only the models with these names and asks the server for the
	// details of these models only. A name that the server does not have
	// is not in the result.
	Models(ctx context.Context, names ...string) ([]domain.Model, error)
	// Running returns the names of the loaded models.
	Running(ctx context.Context) ([]string, error)
	// Load loads model and returns the load time. contextSize must be the
	// ContextSize of the later chats: a server can load a model again when
	// a chat asks for another context size than the load.
	Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error)
	// Unload asks the server to unload model. The model can stay in the
	// running list for a short time after Unload returns.
	Unload(ctx context.Context, model string) error
	// Chat streams one request. progress can be nil.
	Chat(ctx context.Context, req ChatRequest, progress ProgressFunc) (ChatResult, error)
}

var (
	// ErrUnavailable means that the client cannot reach the server.
	ErrUnavailable = errors.New("llm server is not available")
	// ErrModelNotFound means that the server does not have the model.
	ErrModelNotFound = errors.New("model not found")
)
