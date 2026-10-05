package llmtest_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/llm"
	"token-rate/internal/llm/llmtest"
)

var _ llm.Client = (*llmtest.Fake)(nil)

func TestFakeLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := llmtest.New(domain.Model{Name: "a"}, domain.Model{Name: "b"}).WithRunning("b").WithUnloadLag(2)

	if d, err := f.Load(ctx, "a", time.Minute, 8192); err != nil || d != time.Second {
		t.Fatalf("Load() = %v, %v", d, err)
	}
	assertRunning(t, f, "a", "b")
	if err := f.Unload(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	assertRunning(t, f, "a", "b") // lag 2
	assertRunning(t, f, "a", "b") // lag 1
	assertRunning(t, f, "b")
	if _, err := f.Load(ctx, "c", time.Minute, 8192); !errors.Is(err, llm.ErrModelNotFound) {
		t.Errorf("Load(c) error = %v, want ErrModelNotFound", err)
	}
	if got := f.CallsOf(llmtest.MethodLoad); len(got) != 2 || got[0].KeepAlive != time.Minute || got[0].ContextSize != 8192 {
		t.Errorf("CallsOf(Load) = %+v", got)
	}
}

func assertRunning(t *testing.T, f *llmtest.Fake, want ...string) {
	t.Helper()
	got, err := f.Running(context.Background())
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Running() = %v, %v, want %v", got, err, want)
	}
}

func TestFakeDefaultChat(t *testing.T) {
	t.Parallel()
	f := llmtest.New(domain.Model{Name: "a"})
	var calls int
	res, err := f.Chat(context.Background(), llm.ChatRequest{
		Model:        domain.Model{Name: "a"},
		Conversation: domain.Conversation{{Role: domain.RoleUser, Content: "q"}},
		Options:      llm.GenerationOptions{MaxTokens: 8},
	}, func(int) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	m := res.Measurement
	if calls != 8 || m.Server.EvalTokens != 8 || m.DoneReason != domain.DoneLength || m.Server.DecodeRate() != 100 {
		t.Errorf("calls = %d, measurement = %+v", calls, m)
	}
	if res.Reply.Content != "reply to 1 messages" {
		t.Errorf("Reply = %q", res.Reply.Content)
	}
}

func TestFakeErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	f := llmtest.New(domain.Model{Name: "a"}).WithErr(llmtest.MethodVersion, llm.ErrUnavailable).
		WithChat(func(context.Context, llm.ChatRequest, llm.ProgressFunc) (llm.ChatResult, error) {
			return llm.ChatResult{}, boom
		})
	if _, err := f.Version(context.Background()); !errors.Is(err, llm.ErrUnavailable) {
		t.Errorf("Version() error = %v", err)
	}
	if _, err := f.Chat(context.Background(), llm.ChatRequest{Model: domain.Model{Name: "a"}}, nil); !errors.Is(err, boom) {
		t.Errorf("Chat() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Models(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Models() with a canceled context: error = %v", err)
	}
	if len(f.Calls()) != 3 {
		t.Errorf("Calls() has %d calls, want 3", len(f.Calls()))
	}
}

func TestFakeModelsByName(t *testing.T) {
	t.Parallel()
	f := llmtest.New(domain.Model{Name: "a"}, domain.Model{Name: "b"})
	all, err := f.Models(context.Background())
	if err != nil || len(all) != 2 {
		t.Fatalf("Models() = %v, %v", all, err)
	}
	some, err := f.Models(context.Background(), "b", "missing")
	if err != nil || len(some) != 1 || some[0].Name != "b" {
		t.Errorf("Models(b, missing) = %v, %v", some, err)
	}
}
