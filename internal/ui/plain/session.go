package plain

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/report"
	"dyno/internal/ui/format"
)

// SessionUI prints one line for each event of a session, and the summary
// tables after it. It is safe for concurrent use.
type SessionUI struct {
	mu    sync.Mutex
	out   io.Writer
	now   func() time.Time
	lines eventLines
}

// NewSessionUI returns a SessionUI that writes to out. now gives the time
// of day in front of each line.
func NewSessionUI(out io.Writer, now func() time.Time) *SessionUI {
	return &SessionUI{out: out, now: now}
}

// Sink returns the sink that prints the events. It ignores Progress.
func (u *SessionUI) Sink() event.Sink { return event.SinkFunc(u.publish) }

// Run calls session in the calling goroutine and returns its error.
func (u *SessionUI) Run(ctx context.Context, session func(context.Context) error) error {
	return session(ctx)
}

// ShowSummary prints the table of this session, then the table of all
// sessions with the same workload. It drops write errors, because the port
// has no error result.
func (u *SessionUI) ShowSummary(session, allTime report.Report) {
	u.mu.Lock()
	defer u.mu.Unlock()
	_ = writeSection(u.out, format.SessionTitle, session)
	_ = writeSection(u.out, format.AllTimeTitle, allTime)
}

// writeSection writes an empty line and the report, so that the report
// stands apart from the event lines above it.
func writeSection(w io.Writer, title string, r report.Report) error {
	if _, err := io.WriteString(w, "\n"); err != nil {
		return err
	}
	return writeReport(w, title, r)
}

func (u *SessionUI) publish(e event.Event) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.observe(e)
	u.writeLine(u.lines.line(e))
}

// observe keeps the column widths of the schedule for the step lines.
func (u *SessionUI) observe(e event.Event) {
	if s, ok := e.(event.SessionStarted); ok {
		u.lines = newEventLines(s.Steps)
	}
}

// writeLine writes text after the time of day. It skips empty text.
func (u *SessionUI) writeLine(text string) {
	if text == "" {
		return
	}
	fmt.Fprintf(u.out, "%s  %s\n", u.now().Format(time.TimeOnly), text)
}

// eventLines turns events into lines of text. It keeps the column widths
// of the schedule, so that the step lines of one session align.
type eventLines struct{ model, mode, run int }

func newEventLines(steps []domain.Step) eventLines {
	var l eventLines
	for _, s := range steps {
		l.model = max(l.model, utf8.RuneCountInString(s.Model))
		l.mode = max(l.mode, utf8.RuneCountInString(string(s.Mode)))
		l.run = max(l.run, len(strconv.Itoa(s.Run)))
	}
	return l
}

// step returns the aligned label of s, for example
// "qwen3.5:4b-mlx  miss      run 1".
func (l eventLines) step(s domain.Step) string {
	return fmt.Sprintf("%-*s  %-*s  run %*d", l.model, s.Model, l.mode, s.Mode, l.run, s.Run)
}

// line returns the text for e, or "" for an event without a line.
func (l eventLines) line(e event.Event) string {
	switch e := e.(type) {
	case event.SessionStarted:
		return sessionStarted(e)
	case event.ModelLoading:
		return e.Model + " loading"
	case event.ModelReady:
		return e.Model + " loaded in " + format.Duration(e.Load)
	case event.ModelReleased:
		return e.Model + " released"
	case event.Preparing:
		return l.step(e.Step) + "  " + preparing(e)
	case event.StepFinished:
		return l.step(e.Step) + "  " + result(e.Exchange.Measurement)
	case event.StepFailed:
		return l.step(e.Step) + "  " + format.Failed(e.Err)
	case event.Waiting:
		return "next run in " + format.Duration(e.Duration)
	case event.SessionEnded:
		return sessionEnded(e)
	}
	return "" // StepStarted and Progress: the result line follows
}

func sessionStarted(e event.SessionStarted) string {
	parts := []string{
		"session " + string(e.Session.ID),
		format.Count(len(e.Steps), "step"),
		"prompt " + e.Session.Workload.PromptName,
	}
	return joinParts(append(parts, optional("tag ", e.Session.Tag)...))
}

// preparing returns the action, and its count when the step has more than
// one, for example "primer 1/2".
func preparing(e event.Preparing) string {
	if e.Total <= 1 {
		return e.Action
	}
	return fmt.Sprintf("%s %d/%d", e.Action, e.Index, e.Total)
}

// result returns the measured values of a run.
func result(m domain.Measurement) string {
	return joinParts([]string{
		"decode " + format.Rate(report.MetricDecode.Value(m)) + " tok/s",
		"prefill " + format.Millis(report.MetricPrefill.Value(m)) + " ms",
		"ttft " + format.Duration(m.Client.TimeToFirstToken),
		"cached " + format.Percent(m.Server.CachedShare()),
	})
}

func sessionEnded(e event.SessionEnded) string {
	parts := []string{
		"session " + string(e.ID) + " " + string(e.Status),
		format.Count(e.Failed, "failed step"),
	}
	return joinParts(append(parts, optional("error: ", format.SessionError(e.Err))...))
}

// optional returns label and value as one part, or no part when value is
// empty.
func optional(label, value string) []string {
	if value == "" {
		return nil
	}
	return []string{label + value}
}

func joinParts(parts []string) string { return strings.Join(parts, format.Separator) }
