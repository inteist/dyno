package runview

import (
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"dyno/internal/event"
	"dyno/internal/ui/tui/theme"
)

// tickEvery is the interval of the countdown.
const tickEvery = time.Second

// barWidth is the width of the progress bar of one cache mode.
const barWidth = 6

// Model is the Bubble Tea model of the run view.
type Model struct {
	stop    func() // cancels the session; only the first call has an effect
	keys    keyMap
	look    look
	spinner spinner.Model
	state   State
	width   int
	height  int
	now     time.Time // the time of the last tick

	summary    SummaryMsg
	hasSummary bool

	confirming bool // the first q came
	stopping   bool // stop was called
	quitOnDone bool // ctrl+c or StopMsg came, or a quit key before the print
	done       bool // DoneMsg came
	printed    bool // the final frame shows: live, or in the terminal history
	inHistory  bool // the program printed the final frame into the history
	doneErr    error
	quitting   bool
}

// look is the theme and the bubbles that use its styles.
type look struct {
	theme theme.Theme
	bar   progress.Model
	help  help.Model
}

// New returns the model of a session. stop cancels the session context. The
// model calls it at most once.
func New(stop func()) Model {
	if stop == nil {
		stop = func() {}
	}
	m := Model{
		stop:    sync.OnceFunc(stop),
		keys:    newKeyMap(),
		spinner: spinner.New(spinner.WithSpinner(spinner.MiniDot)),
	}
	return m.withTheme(theme.New(true)) // dark until the terminal answers
}

// Init asks for the background color and starts the countdown tick and the
// spinner.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, tick(), m.spinner.Tick)
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		return m.withTheme(theme.New(msg.IsDark())), nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m.refit()
	case tickMsg:
		return m.ticked(time.Time(msg))
	case spinner.TickMsg:
		return m.spun(msg)
	case event.Event:
		m.state = m.state.Apply(msg)
		return m, nil
	case SummaryMsg:
		m.summary, m.hasSummary = msg, true
		return m, nil
	case DoneMsg:
		return m.ended(msg.Err)
	case printedMsg:
		return m.wasPrinted()
	case StopMsg:
		return m.interrupt()
	case tea.KeyPressMsg:
		return m.pressed(msg)
	}
	return m, nil
}

// withTheme returns m with the styles of t.
func (m Model) withTheme(t theme.Theme) Model {
	bar := progress.New(
		progress.WithColors(t.Accent),
		progress.WithFillCharacters('━', '─'),
		progress.WithoutPercentage(),
		progress.WithWidth(barWidth),
	)
	bar.EmptyColor = t.Track
	m.look = look{theme: t, bar: bar, help: newHelp(t)}
	m.spinner.Style = t.Active
	return m
}

// tick returns the command of the next countdown tick.
func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// ticked keeps the time of the tick. The ticks end with the session.
func (m Model) ticked(now time.Time) (tea.Model, tea.Cmd) {
	m.now = now
	if m.done {
		return m, nil
	}
	return m, tick()
}

// spun moves the spinner. The spinner ends with the session.
func (m Model) spun(msg spinner.TickMsg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

// printedMsg comes after the program printed the final frame.
type printedMsg struct{}

// ended records the end of the session function. A final frame that fits
// the window stays the live frame. A taller one goes into the terminal
// history, because Bubble Tea cuts the top lines of a live frame that is
// taller than the window, and it redraws the cut frame into the history.
func (m Model) ended(err error) (tea.Model, tea.Cmd) {
	m.done, m.doneErr, m.confirming = true, err, false
	if !m.finalFits() {
		return m.printFinal()
	}
	m.printed = true
	if m.quitOnDone {
		return m.quit()
	}
	return m, nil
}

// refit prints the live final frame into the history when a resize made
// the window too small for it.
func (m Model) refit() (tea.Model, tea.Cmd) {
	if !m.done || !m.printed || m.inHistory || m.finalFits() {
		return m, nil
	}
	return m.printFinal()
}

// printFinal prints the final frame into the terminal history. The program
// handles the messages of a sequence one after the other, so printedMsg
// comes after the print.
func (m Model) printFinal() (tea.Model, tea.Cmd) {
	m.printed = false
	return m, tea.Sequence(tea.Println(m.finalFrame()), func() tea.Msg { return printedMsg{} })
}

// finalFits reports whether the live final frame, with the key hint, fits
// the window. An unknown height does not fit, so that the frame goes into
// the history.
func (m Model) finalFits() bool {
	live := m
	live.printed, live.inHistory = true, false
	return m.height > 0 && lineCount(live.render()) <= m.height
}

// wasPrinted records the print, and quits after ctrl+c, StopMsg, or a quit
// key that came before the print.
func (m Model) wasPrinted() (tea.Model, tea.Cmd) {
	m.printed, m.inHistory = true, true
	if m.quitOnDone {
		return m.quit()
	}
	return m, nil
}

// quitWhenPrinted quits now if the final frame is in the history, else
// after the print. A quit before the print would lose the final frame.
func (m Model) quitWhenPrinted() (tea.Model, tea.Cmd) {
	if m.printed {
		return m.quit()
	}
	m.quitOnDone = true
	return m, nil
}

// pressed handles a key.
func (m Model) pressed(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.done:
		if key.Matches(k, m.keys.Quit) {
			return m.quitWhenPrinted()
		}
		return m, nil
	case key.Matches(k, m.keys.Interrupt):
		return m.interrupt()
	case key.Matches(k, m.keys.Stop):
		return m.askToStop(), nil
	}
	m.confirming = false // any other key cancels the question
	return m, nil
}

// interrupt stops the session and quits when it returns, or quits at once
// after the session.
func (m Model) interrupt() (tea.Model, tea.Cmd) {
	if m.done {
		return m.quitWhenPrinted()
	}
	m = m.requestStop()
	m.quitOnDone = true
	return m, nil
}

// askToStop asks for a second q, or stops the session after it.
func (m Model) askToStop() Model {
	switch {
	case m.stopping:
		return m
	case m.confirming:
		return m.requestStop()
	}
	m.confirming = true
	return m
}

// requestStop cancels the session. The view shows that it stops.
func (m Model) requestStop() Model {
	m.stop()
	m.stopping, m.confirming = true, false
	return m
}

// quit ends the program. The last frame leaves out the key hint.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.quitting = true
	return m, tea.Quit
}

// hintMode returns the key hint that fits the state of m.
func (m Model) hintMode() hintMode {
	switch {
	case m.quitting, m.done && !m.printed:
		// The frame is empty while the program prints the final frame. The
		// hint then appears below the print, because the frame changes.
		return hintNone
	case m.done:
		return hintDone
	case m.stopping:
		return hintStopping
	case m.confirming:
		return hintConfirm
	}
	return hintRunning
}
