// Package llm is the inference port shared by native Ollama and compatible
// endpoints. Lifecycle ownership is optional; llmtest supplies native test fakes.
package llm

import (
	"context"
	"errors"
	"time"

	"dyno/internal/domain"
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

// ProgressFunc receives the number of streamed content chunks so far, not tokens.
// Authoritative token counts come only from final server usage.
type ProgressFunc func(chunks int)

// Client is what the core needs from an LLM server.
type Client interface {
	// Version returns the server version.
	Version(ctx context.Context) (string, error)
	// Models returns available served IDs and known catalog details. With
	// names, it selects those IDs; unknown metadata stays unavailable, and a
	// name absent from a supported catalog is not in the result.
	Models(ctx context.Context, names ...string) ([]domain.Model, error)
	// Chat streams one request. progress can be nil.
	Chat(ctx context.Context, req ChatRequest, progress ProgressFunc) (ChatResult, error)
}

// ManagedLifecycle is implemented only by servers whose models Dyno owns.
// External endpoint clients must not implement successful no-op lifecycle methods.
type ManagedLifecycle interface {
	// Running returns the names of the loaded models.
	Running(ctx context.Context) ([]string, error)
	// Load loads model and returns the load time. contextSize must be the
	// ContextSize of the later chats: a server can load a model again when
	// a chat asks for another context size than the load.
	Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error)
	// Unload asks the server to unload model. The model can stay in the
	// running list for a short time after Unload returns.
	Unload(ctx context.Context, model string) error
}

// ServerInfo separates protocol, deployment provenance, and measurement semantics.
// Empty optional facts are unknown, not failures of compatible inference.
type ServerInfo struct {
	Backend, ServerKind, Version, VersionSource              string
	Endpoint, MeasurementProtocol, ContextPolicy, ConfigJSON string
}

// InfoProvider supplies optional deployment facts without requiring admin routes.
type InfoProvider interface {
	Info(ctx context.Context) (ServerInfo, error)
}

// NameNormalizer supplies provider-specific model naming rules.
type NameNormalizer interface {
	NormalizeModelName(name string) string
}

// NormalizeModelName delegates explicit naming policy; clients without one use
// opaque IDs, even when they implement managed lifecycle operations.
func NormalizeModelName(client Client, name string) string {
	if n, ok := client.(NameNormalizer); ok {
		return n.NormalizeModelName(name)
	}
	return name
}

// NormalizeModelNames applies one copied selection policy for runner and app.
func NormalizeModelNames(client Client, names []string) []string {
	return domain.NormalizeModelNames(names, func(name string) string { return NormalizeModelName(client, name) })
}

// OptionsValidator rejects unsupported generation/cache policies before recording.
type OptionsValidator interface {
	ValidateOptions(options GenerationOptions, modes []domain.CacheMode) error
}

// CacheResetter performs a real cache reset, never an assumed or no-op reset.
type CacheResetter interface {
	ResetCache(ctx context.Context, model string) error
}

var (
	// ErrUnavailable means that the client cannot reach the server.
	ErrUnavailable = errors.New("llm server is not available")
	// ErrModelNotFound means that the server does not have the model.
	ErrModelNotFound = errors.New("model not found")
)
