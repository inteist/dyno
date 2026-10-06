package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	tea "charm.land/bubbletea/v2"

	"dyno/internal/event"
	"dyno/internal/report"
	"dyno/internal/ui/tui/runview"
)

// SessionUI presents one session with the run view. Bubble Tea runs in the
// calling goroutine, and the session function runs in another goroutine.
// A SessionUI serves one call of Run.
type SessionUI struct {
	in      io.Reader
	out     io.Writer
	sink    *Sink
	options []tea.ProgramOption // more options for the program
}

// NewSessionUI returns a SessionUI that reads keys from in and draws on out.
func NewSessionUI(in io.Reader, out io.Writer) *SessionUI {
	return newSessionUI(in, out)
}

// newSessionUI returns a SessionUI that adds options to the program. Tests
// use it for a window size and a message filter.
func newSessionUI(in io.Reader, out io.Writer, options ...tea.ProgramOption) *SessionUI {
	return &SessionUI{in: in, out: out, sink: NewSink(), options: options}
}

// Sink returns the sink that forwards the events of the runner to the
// program without blocking the runner.
func (u *SessionUI) Sink() event.Sink { return u.sink }

// ShowSummary sends the reports to the program. They arrive after the
// events that came before them.
func (u *SessionUI) ShowSummary(session, allTime report.Report) {
	u.sink.Send(runview.SummaryMsg{Session: session, AllTime: allTime})
}

// Run shows the run view while session runs with a child context of ctx.
// The keys of the view and the end of ctx cancel that context. Run returns
// after both the program and session have ended, with the error of session
// and any failure of the program.
func (u *SessionUI) Run(ctx context.Context, session func(context.Context) error) error {
	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := tea.NewProgram(runview.New(cancel), u.programOptions()...)
	stopFeeding := u.feed(ctx, p)
	result := u.start(sessionCtx, session)

	_, programErr := p.Run()
	cancel() // the session never outlives the program
	sessionErr := <-result
	stopFeeding()
	return runError(sessionErr, programErr)
}

// programOptions returns the options of the program. main cancels ctx on
// SIGINT and SIGTERM, and Run turns that into runview.StopMsg, so that the
// view stops the session and keeps its last frame. The signal handler of
// Bubble Tea would quit the program at once instead. Resize events do not
// depend on that handler.
func (u *SessionUI) programOptions() []tea.ProgramOption {
	options := []tea.ProgramOption{tea.WithInput(u.in), tea.WithOutput(u.out), tea.WithoutSignalHandler()}
	return append(options, u.options...)
}

// feed starts the goroutines that send messages to p: the sink, and
// runview.StopMsg when ctx ends. The returned function stops them and waits
// for them.
func (u *SessionUI) feed(ctx context.Context, p *tea.Program) (stop func()) {
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() { u.sink.Forward(p) })
	wg.Go(func() { stopOnEnd(ctx, done, p) })
	return func() {
		close(done)
		u.sink.Close()
		wg.Wait()
	}
}

// start calls session in a new goroutine. When session returns, it queues
// runview.DoneMsg after the earlier events, then sends the error on the
// returned channel.
func (u *SessionUI) start(ctx context.Context, session func(context.Context) error) <-chan error {
	result := make(chan error, 1)
	go func() {
		err := session(ctx)
		u.sink.Send(runview.DoneMsg{Err: err})
		result <- err
	}()
	return result
}

// stopOnEnd sends runview.StopMsg to p when ctx ends before done closes.
// Send returns at once after the program has ended.
func stopOnEnd(ctx context.Context, done <-chan struct{}, p *tea.Program) {
	select {
	case <-ctx.Done():
		p.Send(runview.StopMsg{})
	case <-done:
	}
}

// runError returns the error of the session, with the error of the program
// when the program failed. A failed program cancels the session, so then
// the cancel is not the cause, and only the program error counts.
func runError(sessionErr, programErr error) error {
	if programErr == nil || errors.Is(programErr, tea.ErrInterrupted) {
		return sessionErr
	}
	programErr = fmt.Errorf("terminal UI: %w", programErr)
	if errors.Is(sessionErr, context.Canceled) {
		return programErr
	}
	return errors.Join(sessionErr, programErr)
}
