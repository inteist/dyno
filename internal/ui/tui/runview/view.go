package runview

import (
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/report"
	"dyno/internal/ui/format"
	"dyno/internal/ui/tui/theme"
)

const (
	defaultWidth = 80  // the width before the first WindowSizeMsg
	margin       = " " // the space in front of each line
	ellipsis     = "…"
	sep          = format.Separator
)

// Marks in front of a model and a step.
const (
	markActive  = "▸"
	markDone    = "✓"
	markPartial = "!"
	markFailed  = "✗"
	markWaiting = "·"
)

// block is a group of lines. Empty lines separate the blocks.
type block struct{ lines []string }

// View returns the inline frame of the session.
func (m Model) View() tea.View { return tea.NewView(m.render()) }

// printingFrame is the frame while the program prints the final frame: one
// blank line. After the print, the frame changes to the key hint, and the
// change makes the renderer draw the hint below the printed lines. Without
// it, a print taller than the screen pushes the hint off the screen.
const printingFrame = " "

// render returns the text of the frame. After a print of the final frame,
// the frame holds only the key hint.
func (m Model) render() string {
	if m.done && !m.printed {
		return printingFrame
	}
	width := m.lineWidth()
	var blocks []block
	if !m.inHistory {
		blocks = m.content(width - len(margin))
	}
	footer := nonEmpty(m.footer(width - len(margin)))
	if m.quitting && !m.inHistory {
		// The last frame keeps the height of the live frame: the key hint
		// becomes an empty line. Bubble Tea erases the last line of an
		// inline frame on exit, and a frame that changes its height leaves
		// its old lines in the history.
		footer = []string{""}
	}
	blocks = append(blocks, block{lines: footer})
	return indent(joinBlocks(blocks), width)
}

// lineCount returns the number of lines of frame.
func lineCount(frame string) int { return strings.Count(frame, "\n") + 1 }

// finalFrame returns the frame of the ended session without the key hint.
// It has no height limit: the program prints it into the terminal history,
// because Bubble Tea cuts the top lines of a frame that is taller than the
// terminal, and those lines never reach the history.
func (m Model) finalFrame() string {
	width := m.lineWidth()
	return indent(joinBlocks(m.content(width-len(margin))), width)
}

// lineWidth returns the width of the window, or defaultWidth before the
// first WindowSizeMsg.
func (m Model) lineWidth() int {
	if m.width <= 0 {
		return defaultWidth
	}
	return m.width
}

// content returns the parts of the frame above the key hint, for a line
// width.
func (m Model) content(width int) []block {
	blocks := []block{{lines: []string{m.title()}}}
	if m.state.Started {
		blocks = append(blocks, block{lines: m.modelTable(width)})
	}
	blocks = append(blocks, block{lines: m.activity()})
	if m.done && m.hasSummary {
		blocks = append(blocks,
			block{lines: m.reportLines(format.SessionTitle, m.summary.Session, width)},
			block{lines: m.reportLines(format.AllTimeTitle, m.summary.AllTime, width)},
		)
	}
	return blocks
}

func joinBlocks(blocks []block) []string {
	var lines []string
	for _, b := range blocks {
		if len(b.lines) == 0 {
			continue
		}
		if lines != nil {
			lines = append(lines, "")
		}
		lines = append(lines, b.lines...)
	}
	return lines
}

// indent puts the margin in front of each line and cuts the lines to width.
func indent(lines []string, width int) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		if l != "" {
			out[i] = ansi.Truncate(margin+l, width, ellipsis)
		}
	}
	return strings.Join(out, "\n")
}

// title returns the first line: the session, the prompt, the tag, the runs,
// and the interval.
func (m Model) title() string {
	t := m.look.theme
	name := t.Title.Render(domain.ToolName)
	if !m.state.Started {
		return name
	}
	s := m.state.Session
	parts := []string{"session " + string(s.ID), "prompt " + s.Workload.PromptName}
	if s.Tag != "" {
		parts = append(parts, "tag "+s.Tag)
	}
	parts = append(parts, format.Count(s.Runs, "run"))
	if s.Interval > 0 {
		parts = append(parts, "interval "+format.Duration(s.Interval))
	}
	return name + t.Muted.Render(sep+strings.Join(parts, sep))
}

// modelTable returns one row for each model. The last-run column takes
// the longest form that fits width, or none.
func (m Model) modelTable(width int) []string {
	base := m.progressTable()
	for _, result := range []func(LastRun) string{modeResult, fullResult, shortResult} {
		if t := base.With("LAST RUN", m.lastRuns(result)); tableWidth(t) <= width {
			return tableLines(t, m.look.theme.Header)
		}
	}
	return tableLines(base, m.look.theme.Header)
}

// progressTable returns the model column and one progress column for each
// cache mode.
func (m Model) progressTable() format.Table {
	t := format.Table{Header: []string{"MODEL"}}
	for _, mode := range m.state.Modes {
		t.Header = append(t.Header, strings.ToUpper(string(mode)))
	}
	for _, row := range m.state.Models {
		cells := []string{m.mark(row) + " " + m.nameStyle(row).Render(row.Name)}
		for _, c := range row.Counts {
			cells = append(cells, m.bar(c))
		}
		t.Rows = append(t.Rows, cells)
	}
	return t
}

// mark returns the status of a model: done, done with failures, active, or
// pending.
func (m Model) mark(row ModelState) string {
	t := m.look.theme
	switch {
	case row.Complete() && row.FailedSteps() > 0:
		return t.Warn.Render(markPartial)
	case row.Complete():
		return t.Good.Render(markDone)
	case m.isCurrent(row):
		return t.Active.Render(markActive)
	}
	return " "
}

func (m Model) nameStyle(row ModelState) lipgloss.Style {
	if m.isCurrent(row) {
		return m.look.theme.Active
	}
	return m.look.theme.Text
}

// isCurrent reports whether the current step belongs to row.
func (m Model) isCurrent(row ModelState) bool {
	c := m.state.Current
	return !m.state.Ended && c.Phase != PhaseNone && c.Step.Model == row.Name
}

// bar returns the progress bar and the count of one cache mode.
func (m Model) bar(c Count) string {
	share := 0.0
	if c.Total > 0 {
		share = float64(c.Done) / float64(c.Total)
	}
	return m.look.bar.ViewAs(share) + " " + m.look.theme.Text.Render(format.Int(c.Done)+"/"+format.Int(c.Total))
}

// lastRuns returns the last run of each model through result.
func (m Model) lastRuns(result func(LastRun) string) []string {
	cells := make([]string, len(m.state.Models))
	for i, row := range m.state.Models {
		if row.HasLast {
			cells[i] = m.look.theme.Text.Render(result(row.Last))
		}
	}
	return cells
}

// The forms of the last-run column, from the longest to the shortest.
func modeResult(r LastRun) string  { return string(r.Mode) + ": " + runResult(r.Measurement) }
func fullResult(r LastRun) string  { return runResult(r.Measurement) }
func shortResult(r LastRun) string { return rateText(r.Measurement) }

// runResult returns the decode rate and the TTFT of a run, for example
// "56.0 tok/s · ttft 71 ms".
func runResult(m domain.Measurement) string {
	return rateText(m) + sep + "ttft " + format.Duration(m.Client.TimeToFirstToken)
}

// rateText returns the decode rate of a run, for example "56.0 tok/s".
func rateText(m domain.Measurement) string {
	return format.Rate(report.MetricDecode.Value(m)) + " tok/s"
}

// activity returns the current step and the failures while the session
// runs, and the final status after it.
func (m Model) activity() []string {
	if m.done || m.state.Ended {
		return m.statusLines()
	}
	return nonEmpty(m.stepLine(), m.failureLine())
}

// stepLine returns what the current step does.
func (m Model) stepLine() string {
	t := m.look.theme
	c := m.state.Current
	switch c.Phase {
	case PhaseNone:
		return m.spinner.View() + " " + t.Text.Render("starting the session")
	case PhaseWaiting:
		return m.waitLine()
	case PhaseFinished:
		return t.Good.Render(markDone) + " " + t.Text.Render(stepLabel(c.Step)+sep+runResult(c.Result))
	case PhaseFailed:
		return t.Bad.Render(markFailed + " " + stepLabel(c.Step) + sep + format.Failed(c.Err))
	}
	return m.spinner.View() + " " + t.Text.Render(stepLabel(c.Step)+sep+m.phaseText(c))
}

// waitLine names the next step.
func (m Model) waitLine() string {
	t := m.look.theme
	next, ok := m.state.Next()
	if !ok {
		return t.Muted.Render(markWaiting + " waiting")
	}
	return t.Muted.Render(markWaiting+" next ") + t.Text.Render(stepLabel(next))
}

// phaseText returns the phase of a step that runs.
func (m Model) phaseText(c Current) string {
	switch c.Phase {
	case PhaseLoading:
		return "loading the model"
	case PhaseWarmup:
		return event.ActionWarmup
	case PhaseReload:
		return event.ActionReload
	case PhasePrimer:
		return primerText(c)
	case PhaseRunning:
		return m.tokensText(c)
	}
	return "starting"
}

// primerText returns "primer", or "primer 1/2" when the step has more than
// one primer.
func primerText(c Current) string {
	if c.Actions <= 1 {
		return event.ActionPrimer
	}
	return event.ActionPrimer + " " + format.Int(c.Action) + "/" + format.Int(c.Actions)
}

// tokensText returns the tokens so far, out of num_predict, and the live
// rate.
func (m Model) tokensText(c Current) string {
	tokens := format.Count(c.Tokens, "token")
	if limit := m.state.Session.Workload.NumPredict; limit > 0 {
		tokens = format.Int(c.Tokens) + "/" + format.Int(limit) + " tokens"
	}
	if rate, ok := c.Rate(); ok {
		return tokens + sep + format.Rate(rate) + " tok/s"
	}
	return tokens
}

// failureLine counts the failed steps and shows the last error.
func (m Model) failureLine() string {
	s := m.state
	if s.Failed == 0 {
		return ""
	}
	text := format.Count(s.Failed, "failed step")
	if s.LastErr != nil {
		text += sep + "last error: " + format.Error(s.LastErr)
	}
	return m.look.theme.Bad.Render(text)
}

// statusLines returns the final status and the error.
func (m Model) statusLines() []string { return nonEmpty(m.statusLine(), m.errorLine()) }

// statusLine returns the status of the ended session, or that the session
// did not start.
func (m Model) statusLine() string {
	t := m.look.theme
	switch {
	case m.state.Ended:
		e := m.state.End
		return statusStyle(t, e).Render("session " + string(e.ID) + " " + string(e.Status) + sep + format.Count(e.Failed, "failed step"))
	case !m.state.Started:
		return t.Bad.Render("the session did not start")
	}
	return ""
}

// errorLine returns the error of the session, or "" without one. After
// DoneMsg, it is the error of the session function.
func (m Model) errorLine() string {
	err := m.state.End.Err
	if m.done {
		err = m.doneErr
	}
	text := format.SessionError(err)
	if text == "" {
		return ""
	}
	return m.look.theme.Bad.Render("error: " + text)
}

// statusStyle returns Good for a session without problems, Bad for a
// failed session, and Warn otherwise.
func statusStyle(t theme.Theme, e event.SessionEnded) lipgloss.Style {
	switch {
	case e.Status == domain.StatusFailed:
		return t.Bad
	case e.Status == domain.StatusCompleted && e.Failed == 0:
		return t.Good
	}
	return t.Warn
}

// footer returns the step, the countdown, and the key hint. After the
// session, it is the key hint only.
func (m Model) footer(width int) string {
	keys := hint(m.hintMode(), m.keys, m.look.help, m.look.theme)
	if m.done {
		return keys
	}
	left := m.look.theme.Muted.Render(strings.Join(m.footerParts(), sep))
	return format.Justify(left, keys, width, len(gap), lipgloss.Width)
}

// footerParts returns the step number and the countdown of a wait.
func (m Model) footerParts() []string {
	c := m.state.Current
	var parts []string
	if c.Total > 0 {
		parts = append(parts, "step "+format.Int(c.Index+1)+"/"+format.Int(c.Total))
	}
	if c.Phase == PhaseWaiting && !m.state.Ended {
		parts = append(parts, countdownText(c.Remaining(m.now)))
	}
	return parts
}

// countdownText returns "next run in 18 s", or "next run now" at the end
// of the wait.
func countdownText(left time.Duration) string {
	if left <= 0 {
		return "next run now"
	}
	return "next run in " + format.Duration(left)
}

// stepLabel returns "qwen3.5:4b-mlx · prefix · run 2".
func stepLabel(s domain.Step) string {
	return s.Model + sep + string(s.Mode) + sep + "run " + format.Int(s.Run)
}

// nonEmpty returns the lines that are not empty.
func nonEmpty(lines ...string) []string {
	return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
}
