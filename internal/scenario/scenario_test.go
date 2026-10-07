package scenario

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"dyno/internal/domain"
	"dyno/internal/prompt"
)

// seqIDs gives the IDs "ID1", "ID2", and so on.
type seqIDs struct{ n int }

func (s *seqIDs) Next() string { s.n++; return fmt.Sprintf("ID%d", s.n) }

// call is one recorded call of fakeExec.
type call struct {
	reset     bool
	conv      domain.Conversation
	maxTokens int
}

// fakeExec records the calls of a scenario. The call with the 1-based index
// failAt returns err.
type fakeExec struct {
	calls  []call
	failAt int
	err    error
}

func (f *fakeExec) ResetCache(context.Context) error {
	return f.record(call{reset: true})
}

func (f *fakeExec) Prime(_ context.Context, conv domain.Conversation, maxTokens int) (domain.Message, error) {
	if err := f.record(call{conv: conv, maxTokens: maxTokens}); err != nil {
		return domain.Message{}, err
	}
	return domain.Message{Role: domain.RoleAssistant, Content: "answer"}, nil
}

func (f *fakeExec) record(c call) error {
	f.calls = append(f.calls, c)
	if len(f.calls) == f.failAt {
		return f.err
	}
	return nil
}

func fullSet() prompt.Set {
	return prompt.Set{Name: "full", System: "P", Question: "Q", FollowUp: "F"}
}

func system(content string) domain.Message {
	return domain.Message{Role: domain.RoleSystem, Content: content}
}

func user(content string) domain.Message {
	return domain.Message{Role: domain.RoleUser, Content: content}
}

func mustNew(t *testing.T, mode domain.CacheMode, set prompt.Set) Scenario {
	t.Helper()
	s, err := New(mode, set, &seqIDs{})
	if err != nil {
		t.Fatalf("New(%s) = %v", mode, err)
	}
	return s
}

func TestFirstTurn(t *testing.T) {
	t.Parallel()
	noSystem := prompt.Set{Question: "Q"}
	tests := []struct {
		name string
		set  prompt.Set
		pos  idPosition
		want domain.Conversation
	}{
		{"before prefix", fullSet(), beforePrefix, domain.Conversation{system("[run X] P"), user("Q")}},
		{"before prefix without system", noSystem, beforePrefix, domain.Conversation{user("[run X] Q")}},
		{"before question", fullSet(), beforeQuestion, domain.Conversation{system("P"), user("[run X] Q")}},
		{"before question without system", noSystem, beforeQuestion, domain.Conversation{user("[run X] Q")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := firstTurn(tt.set, "X", tt.pos); !slices.Equal(got, tt.want) {
				t.Errorf("firstTurn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		mode    domain.CacheMode
		primers int
	}{
		{domain.CacheMiss, 0},
		{domain.CacheContinue, 1},
		{domain.CachePrefix, 2},
	} {
		s := mustNew(t, tt.mode, fullSet())
		if s.Mode() != tt.mode || s.Primers() != tt.primers {
			t.Errorf("New(%s): Mode() = %s, Primers() = %d, want %d", tt.mode, s.Mode(), s.Primers(), tt.primers)
		}
	}
}

func TestNewRejects(t *testing.T) {
	t.Parallel()
	short := prompt.Set{Name: "short", Question: "Q"}
	tests := []struct {
		name string
		mode domain.CacheMode
		set  prompt.Set
		ids  prompt.IDSource
		want string
	}{
		{"unknown mode", "hit", fullSet(), &seqIDs{}, `unknown cache mode "hit"`},
		{"continue without system", domain.CacheContinue, short, &seqIDs{}, `prompt "short" cannot serve the cache mode continue`},
		{"prefix without system", domain.CachePrefix, short, &seqIDs{}, `prompt "short" cannot serve the cache mode prefix`},
		{"miss without question", domain.CacheMiss, prompt.Set{Name: "empty"}, &seqIDs{}, `prompt "empty" cannot serve the cache mode miss`},
		{"no ID source", domain.CacheMiss, fullSet(), nil, "ID source is nil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s, err := New(tt.mode, tt.set, tt.ids)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("New() = %v, %v, want an error with %q", s, err, tt.want)
			}
		})
	}
}

func TestMiss(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		set  prompt.Set
		want domain.Conversation
	}{
		{"with system", fullSet(), domain.Conversation{system("[run ID1] P"), user("Q")}},
		{"without system", prompt.Set{Question: "Q"}, domain.Conversation{user("[run ID1] Q")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := &fakeExec{}
			conv, err := mustNew(t, domain.CacheMiss, tt.set).Prepare(context.Background(), exec)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(conv, tt.want) {
				t.Errorf("Prepare() = %v, want %v", conv, tt.want)
			}
			if len(exec.calls) != 0 {
				t.Errorf("miss made %d calls, want none", len(exec.calls))
			}
		})
	}
}

func TestMissGivesNewIDs(t *testing.T) {
	t.Parallel()
	s := mustNew(t, domain.CacheMiss, fullSet())
	a, _ := s.Prepare(context.Background(), &fakeExec{})
	b, _ := s.Prepare(context.Background(), &fakeExec{})
	if a[0] == b[0] {
		t.Errorf("two miss turns have the same system part %q", a[0].Content)
	}
}

func TestContinue(t *testing.T) {
	t.Parallel()
	exec := &fakeExec{}
	conv, err := mustNew(t, domain.CacheContinue, fullSet()).Prepare(context.Background(), exec)
	if err != nil {
		t.Fatal(err)
	}
	first := domain.Conversation{system("[run ID1] P"), user("Q")}
	want := first.With(domain.Message{Role: domain.RoleAssistant, Content: "answer"}, user("F"))
	if !slices.Equal(conv, want) {
		t.Errorf("Prepare() = %v, want %v", conv, want)
	}
	if len(exec.calls) != 1 {
		t.Fatalf("calls = %v, want one Prime", exec.calls)
	}
	if c := exec.calls[0]; c.reset || c.maxTokens != 64 || !slices.Equal(c.conv, first) {
		t.Errorf("Prime call = %+v, want the first turn with 64 tokens", c)
	}
}

func TestPrefix(t *testing.T) {
	t.Parallel()
	exec := &fakeExec{}
	conv, err := mustNew(t, domain.CachePrefix, fullSet()).Prepare(context.Background(), exec)
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.calls) != 3 || !exec.calls[0].reset {
		t.Fatalf("calls = %+v, want ResetCache and two Prime calls", exec.calls)
	}
	convs := []domain.Conversation{exec.calls[1].conv, exec.calls[2].conv, conv}
	for i, c := range exec.calls[1:] {
		if c.reset || c.maxTokens != 1 {
			t.Errorf("call %d = %+v, want Prime with 1 token", i+2, c)
		}
	}
	for i, c := range convs {
		want := domain.Conversation{system("P"), user(fmt.Sprintf("[run ID%d] Q", i+1))}
		if !slices.Equal(c, want) {
			t.Errorf("conversation %d = %v, want %v", i+1, c, want)
		}
	}
}

func TestPrepareErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name   string
		mode   domain.CacheMode
		failAt int
		want   string
	}{
		{"continue primer", domain.CacheContinue, 1, "first turn: boom"},
		{"prefix reset", domain.CachePrefix, 1, "reset cache: boom"},
		{"prefix primer 1", domain.CachePrefix, 2, "primer 1: boom"},
		{"prefix primer 2", domain.CachePrefix, 3, "primer 2: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			exec := &fakeExec{failAt: tt.failAt, err: boom}
			conv, err := mustNew(t, tt.mode, fullSet()).Prepare(context.Background(), exec)
			if !errors.Is(err, boom) || err.Error() != tt.want {
				t.Errorf("Prepare() error = %v, want %q", err, tt.want)
			}
			if conv != nil {
				t.Errorf("Prepare() = %v, want no conversation", conv)
			}
			if len(exec.calls) != tt.failAt {
				t.Errorf("%d calls, want %d: the preparation must stop at the error", len(exec.calls), tt.failAt)
			}
		})
	}
}
