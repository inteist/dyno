// Package llmtest gives a scriptable fake of llm.Client for tests.
package llmtest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// Methods of llm.Client, as Call.Method records them.
const (
	MethodVersion = "Version"
	MethodModels  = "Models"
	MethodRunning = "Running"
	MethodLoad    = "Load"
	MethodUnload  = "Unload"
	MethodChat    = "Chat"
)

// ChatFunc answers one chat request of a Fake.
type ChatFunc func(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error)

// Call is one recorded call of a Fake.
type Call struct {
	Method      string
	Model       string // empty for Version, Models, and Running
	KeepAlive   time.Duration
	ContextSize int             // only for Load
	Request     llm.ChatRequest // only for Chat
}

// Fake is a scriptable llm.Client. It keeps the set of loaded models, so that
// Load, Unload, and Running agree, and it records every call. Configure it
// with the With methods before the first call. It is safe for concurrent use.
type Fake struct {
	mu        sync.Mutex
	version   string
	models    []domain.Model
	running   []string
	lagging   map[string]int // Running calls that still list an unloaded model
	unloadLag int
	loadTime  time.Duration
	errs      map[string]error
	chat      ChatFunc
	calls     []Call
}

// New returns a fake server with models, version "0.34.0", no loaded model,
// a load time of 1 s, and DefaultChat.
func New(models ...domain.Model) *Fake {
	return &Fake{
		version:  "0.34.0",
		models:   slices.Clone(models),
		lagging:  map[string]int{},
		loadTime: time.Second,
		errs:     map[string]error{},
		chat:     DefaultChat,
	}
}

// WithVersion sets the server version.
func (f *Fake) WithVersion(v string) *Fake { f.version = v; return f }

// WithRunning marks models as loaded before the first call.
func (f *Fake) WithRunning(names ...string) *Fake { f.running = slices.Clone(names); return f }

// WithLoadTime sets the load time that Load returns.
func (f *Fake) WithLoadTime(d time.Duration) *Fake { f.loadTime = d; return f }

// WithUnloadLag makes Running list a model for n more calls after Unload.
func (f *Fake) WithUnloadLag(n int) *Fake { f.unloadLag = n; return f }

// WithChat replaces DefaultChat.
func (f *Fake) WithChat(fn ChatFunc) *Fake { f.chat = fn; return f }

// WithErr makes every call of method return err. method is one of the
// Method constants.
func (f *Fake) WithErr(method string, err error) *Fake { f.errs[method] = err; return f }

// Calls returns a copy of the recorded calls in order.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// CallsOf returns the recorded calls of method in order.
func (f *Fake) CallsOf(method string) []Call {
	var out []Call
	for _, c := range f.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// Version implements llm.Client.
func (f *Fake) Version(ctx context.Context) (string, error) {
	if err := f.begin(ctx, Call{Method: MethodVersion}); err != nil {
		return "", err
	}
	return f.version, nil
}

// Models implements llm.Client.
func (f *Fake) Models(ctx context.Context, names ...string) ([]domain.Model, error) {
	if err := f.begin(ctx, Call{Method: MethodModels}); err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return slices.Clone(f.models), nil
	}
	var out []domain.Model
	for _, m := range f.models {
		if slices.Contains(names, m.Name) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Running implements llm.Client.
func (f *Fake) Running(ctx context.Context) ([]string, error) {
	if err := f.begin(ctx, Call{Method: MethodRunning}); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	names := slices.Clone(f.running)
	for name, left := range f.lagging {
		names = append(names, name)
		if left <= 1 {
			delete(f.lagging, name)
		} else {
			f.lagging[name] = left - 1
		}
	}
	slices.Sort(names)
	return names, nil
}

// Load implements llm.Client.
func (f *Fake) Load(ctx context.Context, model string, keepAlive time.Duration, contextSize int) (time.Duration, error) {
	if err := f.begin(ctx, Call{Method: MethodLoad, Model: model, KeepAlive: keepAlive, ContextSize: contextSize}); err != nil {
		return 0, err
	}
	if err := f.known(model); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.Contains(f.running, model) {
		f.running = append(f.running, model)
	}
	return f.loadTime, nil
}

// Unload implements llm.Client.
func (f *Fake) Unload(ctx context.Context, model string) error {
	if err := f.begin(ctx, Call{Method: MethodUnload, Model: model}); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := slices.Index(f.running, model); i >= 0 {
		f.running = slices.Delete(f.running, i, i+1)
		if f.unloadLag > 0 {
			f.lagging[model] = f.unloadLag
		}
	}
	return nil
}

// Chat implements llm.Client.
func (f *Fake) Chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	if err := f.begin(ctx, Call{Method: MethodChat, Model: req.Model.Name, KeepAlive: req.KeepAlive, Request: req}); err != nil {
		return llm.ChatResult{}, err
	}
	if err := f.known(req.Model.Name); err != nil {
		return llm.ChatResult{}, err
	}
	return f.chat(ctx, req, progress)
}

// begin records c and returns the configured error or the context error.
func (f *Fake) begin(ctx context.Context, c Call) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.errs[c.Method]
}

func (f *Fake) known(model string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !slices.ContainsFunc(f.models, func(m domain.Model) bool { return m.Name == model }) {
		return fmt.Errorf("%s: %w", model, llm.ErrModelNotFound)
	}
	return nil
}

// Timings of DefaultChat.
const (
	DefaultPromptDuration = 100 * time.Millisecond
	DefaultTokenDuration  = 10 * time.Millisecond // 100 tokens per second
	DefaultTTFT           = 120 * time.Millisecond
)

// DefaultChat answers with MaxTokens tokens at 100 tokens per second and
// done reason "length". It counts one prompt token for each message and
// calls progress once for each token. Its reply names the number of
// messages, for example "reply to 2 messages".
func DefaultChat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	tokens := req.Options.MaxTokens
	for i := 1; i <= tokens && progress != nil; i++ {
		progress(i)
	}
	eval := time.Duration(tokens) * DefaultTokenDuration
	return llm.ChatResult{
		Reply: domain.Message{Role: domain.RoleAssistant, Content: fmt.Sprintf("reply to %d messages", len(req.Conversation))},
		Measurement: domain.Measurement{
			Server: domain.ServerMetrics{
				PromptTokens:   len(req.Conversation),
				PromptDuration: DefaultPromptDuration,
				EvalTokens:     tokens,
				EvalDuration:   eval,
				TotalDuration:  DefaultPromptDuration + eval,
			},
			Client:     domain.ClientMetrics{TimeToFirstToken: DefaultTTFT, Wall: DefaultPromptDuration + eval},
			DoneReason: domain.DoneLength,
		},
	}, nil
}
