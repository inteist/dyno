package tui

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"token-rate/internal/domain"
	"token-rate/internal/event"
	"token-rate/internal/report"
	"token-rate/internal/ui/tui/runview"
)

// sessionUI copies the SessionUI port of plan section 8.7, which the app
// package declares.
type sessionUI interface {
	Sink() event.Sink
	Run(ctx context.Context, session func(context.Context) error) error
	ShowSummary(session, allTime report.Report)
}

var _ sessionUI = (*SessionUI)(nil)

var errSession = errors.New("2 of 8 steps failed")

// msgLog records the messages of the session in the order in which the
// program reads them.
type msgLog struct {
	mu   sync.Mutex
	msgs []tea.Msg
	done chan struct{} // closes when DoneMsg arrives
}

func newMsgLog() *msgLog { return &msgLog{done: make(chan struct{})} }

// filter is a tea.WithFilter function that records the messages.
func (l *msgLog) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case event.Event, runview.SummaryMsg, runview.StopMsg, runview.DoneMsg:
		l.mu.Lock()
		l.msgs = append(l.msgs, msg)
		l.mu.Unlock()
	}
	if _, ok := msg.(runview.DoneMsg); ok {
		close(l.done)
	}
	return msg
}

// names returns the types of the recorded messages.
func (l *msgLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, len(l.msgs))
	for i, msg := range l.msgs {
		names[i] = fmt.Sprintf("%T", msg)
	}
	return names
}

// assertClosed fails when ended is still open, which means that the session
// goroutine outlived Run.
func assertClosed(t *testing.T, ended <-chan struct{}) {
	t.Helper()
	select {
	case <-ended:
	default:
		t.Error("the session still runs after Run returned")
	}
}

// testUI returns a SessionUI that needs no terminal.
func testUI(in io.Reader, out io.Writer, options ...tea.ProgramOption) *SessionUI {
	return newSessionUI(in, out, append([]tea.ProgramOption{tea.WithWindowSize(80, 24)}, options...)...)
}

func TestRunShowsTheSessionAndReturnsItsError(t *testing.T) {
	t.Parallel()
	keys, typing := io.Pipe()
	t.Cleanup(func() { _ = typing.Close() })
	log := newMsgLog()
	var out syncWriter
	ui := testUI(keys, &out, tea.WithFilter(log.filter))
	go func() {
		<-log.done
		_, _ = typing.Write([]byte("q")) // q quits after the session
	}()

	s := domain.Step{Model: "qwen3.5:4b-mlx", Mode: domain.CacheMiss, Run: 1}
	ended := make(chan struct{})
	err := within(t, func() error {
		return ui.Run(t.Context(), func(context.Context) error {
			defer close(ended)
			sink := ui.Sink()
			sink.Publish(event.SessionStarted{Session: domain.Session{ID: "S"}, Steps: []domain.Step{s}})
			sink.Publish(event.StepStarted{Index: 0, Total: 1, Step: s})
			sink.Publish(event.StepFailed{Step: s, Err: errSession})
			sink.Publish(event.SessionEnded{ID: "S", Status: domain.StatusCompleted, Failed: 1, Err: errSession})
			ui.ShowSummary(report.Report{Rows: 1}, report.Report{Rows: 2})
			return errSession
		})
	})
	if !errors.Is(err, errSession) {
		t.Errorf("Run() = %v, want %v", err, errSession)
	}
	assertClosed(t, ended)
	want := []string{
		"event.SessionStarted", "event.StepStarted", "event.StepFailed", "event.SessionEnded",
		"runview.SummaryMsg", "runview.DoneMsg",
	}
	if got := log.names(); !slices.Equal(got, want) {
		t.Errorf("the program read %q, want %q", got, want)
	}
	if frame := ansi.Strip(out.String()); !strings.Contains(frame, "this session") {
		t.Errorf("the output lacks the summary:\n%s", frame)
	}
}

func TestRunStopsTheSessionWhenTheParentContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	log := newMsgLog()
	ui := testUI(nil, &syncWriter{}, tea.WithFilter(log.filter))
	running, ended := make(chan struct{}), make(chan struct{})
	go func() {
		<-running
		cancel()
	}()
	err := within(t, func() error {
		return ui.Run(ctx, func(ctx context.Context) error {
			defer close(ended)
			close(running)
			<-ctx.Done()
			return ctx.Err()
		})
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, want context.Canceled", err)
	}
	assertClosed(t, ended)
	if got := log.names(); !slices.Contains(got, "runview.StopMsg") {
		t.Errorf("the program read %q, want a StopMsg", got)
	}
}

func TestRunStopsTheSessionOnKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		keys  string // typed while the session runs
		after string // typed after the session
	}{
		{"ctrl+c quits after the session", "\x03", ""},
		{"q q shows the end", "qq", "q"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			keys, typing := io.Pipe()
			t.Cleanup(func() { _ = typing.Close() })
			log := newMsgLog()
			ui := testUI(keys, &syncWriter{}, tea.WithFilter(log.filter))
			running, ended := make(chan struct{}), make(chan struct{})
			go func() {
				<-running
				_, _ = typing.Write([]byte(tt.keys))
				<-log.done
				if tt.after != "" {
					_, _ = typing.Write([]byte(tt.after))
				}
			}()
			err := within(t, func() error {
				return ui.Run(t.Context(), func(ctx context.Context) error {
					defer close(ended)
					close(running)
					<-ctx.Done()
					return ctx.Err()
				})
			})
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Run() = %v, want context.Canceled", err)
			}
			assertClosed(t, ended)
		})
	}
}

func TestRunEndsTheSessionWhenTheProgramEnds(t *testing.T) {
	t.Parallel()
	quitOnEvent := func(_ tea.Model, msg tea.Msg) tea.Msg {
		if _, ok := msg.(event.Event); ok {
			return tea.QuitMsg{}
		}
		return msg
	}
	ui := testUI(nil, &syncWriter{}, tea.WithFilter(quitOnEvent))
	ended := make(chan struct{})
	err := within(t, func() error {
		return ui.Run(t.Context(), func(ctx context.Context) error {
			defer close(ended)
			ui.Sink().Publish(event.ModelLoading{Model: "m"})
			<-ctx.Done()
			// The runner keeps publishing while it stops. No call may
			// block after the program has ended.
			for range 2 * sinkBuffer {
				ui.Sink().Publish(event.ModelReleased{Model: "m"})
			}
			ui.ShowSummary(report.Report{}, report.Report{})
			return ctx.Err()
		})
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, want context.Canceled", err)
	}
	assertClosed(t, ended)
}

func TestRunReportsAFailedProgram(t *testing.T) {
	t.Parallel()
	ui := testUI(failingReader{}, &syncWriter{})
	ended := make(chan struct{})
	err := within(t, func() error {
		return ui.Run(t.Context(), func(ctx context.Context) error {
			defer close(ended)
			<-ctx.Done()
			return ctx.Err()
		})
	})
	if !errors.Is(err, errBrokenInput) {
		t.Errorf("Run() = %v, want %v", err, errBrokenInput)
	}
	if errors.Is(err, context.Canceled) {
		t.Errorf("Run() = %v, which looks like a stop by the user", err)
	}
	assertClosed(t, ended)
}

func TestRunError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		session, program   error
		want               []error
		canceled, joinedUI bool
	}{
		{name: "no errors"},
		{name: "session error", session: errSession, want: []error{errSession}},
		{name: "stopped", session: context.Canceled, want: []error{context.Canceled}, canceled: true},
		{name: "interrupted program", session: errSession, program: tea.ErrInterrupted, want: []error{errSession}},
		{name: "failed program stops the session", session: context.Canceled, program: errBrokenInput, want: []error{errBrokenInput}},
		{name: "failed program and session", session: errSession, program: errBrokenInput, want: []error{errSession, errBrokenInput}},
	}
	for _, tt := range tests {
		got := runError(tt.session, tt.program)
		if len(tt.want) == 0 && got != nil {
			t.Errorf("%s: runError() = %v, want nil", tt.name, got)
		}
		for _, w := range tt.want {
			if !errors.Is(got, w) {
				t.Errorf("%s: runError() = %v, want %v in it", tt.name, got, w)
			}
		}
		if errors.Is(got, context.Canceled) != tt.canceled {
			t.Errorf("%s: runError() = %v, canceled %v, want %v", tt.name, got, !tt.canceled, tt.canceled)
		}
	}
}

func TestNewSessionUIUsesTheStreams(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	in := strings.NewReader("")
	ui := NewSessionUI(in, &out)
	if ui.in != in || ui.out != &out || ui.sink == nil || len(ui.options) != 0 {
		t.Errorf("NewSessionUI() = %+v", ui)
	}
	if ui.Sink() != event.Sink(ui.sink) {
		t.Error("Sink() is not the sink of the UI")
	}
	if got := len(ui.programOptions()); got != 3 {
		t.Errorf("the program has %d options, want input, output, and no signal handler", got)
	}
}
