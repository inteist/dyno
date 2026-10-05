package runview

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// press returns the message of a key: a letter, "enter", "esc", or
// "ctrl+c".
func press(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// stopCounter counts the calls of stop.
type stopCounter struct{ calls int }

func (c *stopCounter) stop() { c.calls++ }

// isQuit reports whether cmd quits, also as a part of a tea.Sequence.
func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); ok {
		return true
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice {
		for i := range v.Len() {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok && isQuit(c) {
				return true
			}
		}
	}
	return false
}

func TestPressKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		msgs  []tea.Msg
		stops int
		quit  bool
		mode  hintMode
	}{
		{"one q asks", []tea.Msg{press("q")}, 0, false, hintConfirm},
		{"two q stop", []tea.Msg{press("q"), press("q")}, 1, false, hintStopping},
		{"more q stop once", []tea.Msg{press("q"), press("q"), press("q"), press("q")}, 1, false, hintStopping},
		{"another key cancels the question", []tea.Msg{press("q"), press("x"), press("q")}, 0, false, hintConfirm},
		{"enter cancels the question", []tea.Msg{press("q"), press("enter")}, 0, false, hintRunning},
		{"ctrl+c stops at once", []tea.Msg{press("ctrl+c")}, 1, false, hintStopping},
		{"ctrl+c after q q stops once", []tea.Msg{press("q"), press("q"), press("ctrl+c")}, 1, false, hintStopping},
		{"ctrl+c quits after the session", []tea.Msg{press("ctrl+c"), DoneMsg{Err: errSteps}, printedMsg{}}, 1, true, hintNone},
		{"ctrl+c waits for the print", []tea.Msg{press("ctrl+c"), DoneMsg{Err: errSteps}}, 1, false, hintNone},
		{"StopMsg acts like ctrl+c", []tea.Msg{StopMsg{}}, 1, false, hintStopping},
		{"StopMsg quits after the session", []tea.Msg{StopMsg{}, StopMsg{}, DoneMsg{}, printedMsg{}}, 1, true, hintNone},
		{"q q waits after the session", []tea.Msg{press("q"), press("q"), DoneMsg{}, printedMsg{}}, 1, false, hintDone},
		{"no hint during the print", []tea.Msg{DoneMsg{}}, 0, false, hintNone},
		{"the end cancels the question", []tea.Msg{press("q"), DoneMsg{}, printedMsg{}}, 0, false, hintDone},
		{"q quits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, press("q")}, 0, true, hintNone},
		{"q before the print waits", []tea.Msg{DoneMsg{}, press("q")}, 0, false, hintNone},
		{"the print after q quits", []tea.Msg{DoneMsg{}, press("q"), printedMsg{}}, 0, true, hintNone},
		{"enter quits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, press("enter")}, 0, true, hintNone},
		{"esc quits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, press("esc")}, 0, true, hintNone},
		{"ctrl+c quits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, press("ctrl+c")}, 0, true, hintNone},
		{"StopMsg quits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, StopMsg{}}, 0, true, hintNone},
		{"another key waits after the end", []tea.Msg{DoneMsg{}, printedMsg{}, press("x")}, 0, false, hintDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var c stopCounter
			m, cmd := feed(t, New(c.stop), tt.msgs...)
			if c.calls != tt.stops {
				t.Errorf("stop was called %d times, want %d", c.calls, tt.stops)
			}
			if got := isQuit(cmd); got != tt.quit {
				t.Errorf("the last command quits: %v, want %v", got, tt.quit)
			}
			if got := m.hintMode(); got != tt.mode {
				t.Errorf("hint mode = %d, want %d", got, tt.mode)
			}
		})
	}
}

func TestStopRunsOnceForCopiesOfTheModel(t *testing.T) {
	t.Parallel()
	var c stopCounter
	asked, _ := feed(t, New(c.stop), press("q"))
	feed(t, asked, press("q"))
	feed(t, asked, press("q"))
	feed(t, asked, press("ctrl+c"))
	if c.calls != 1 {
		t.Errorf("stop was called %d times, want 1", c.calls)
	}
}

func TestNewWithoutStop(t *testing.T) {
	t.Parallel()
	m, _ := feed(t, New(nil), press("ctrl+c"))
	if !m.stopping {
		t.Error("ctrl+c did not stop")
	}
}

func TestInit(t *testing.T) {
	t.Parallel()
	cmd := New(nil).Init()
	if cmd == nil {
		t.Fatal("Init returned no command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("Init returned %T, want a batch", cmd())
	}
	if len(batch) != 3 {
		t.Fatalf("the batch has %d commands, want 3", len(batch))
	}
	want := reflect.TypeOf(tea.RequestBackgroundColor())
	if got := reflect.TypeOf(batch[0]()); got != want {
		t.Errorf("the first command returns %v, want %v", got, want)
	}
}

func TestTicksEndWithTheSession(t *testing.T) {
	t.Parallel()
	now := start.Add(time.Minute)
	m, cmd := feed(t, New(nil), tickMsg(now))
	if cmd == nil {
		t.Error("a tick did not ask for the next tick")
	}
	if !m.now.Equal(now) {
		t.Errorf("now = %v, want %v", m.now, now)
	}
	frame := m.spinner.View()
	m, cmd = feed(t, m, m.spinner.Tick())
	if cmd == nil || m.spinner.View() == frame {
		t.Error("a spinner tick did not move the spinner")
	}
	m, _ = feed(t, m, DoneMsg{})
	if _, cmd = feed(t, m, tickMsg(now)); cmd != nil {
		t.Error("a tick after the session asked for the next tick")
	}
	if _, cmd = feed(t, m, spinner.TickMsg{ID: m.spinner.ID()}); cmd != nil {
		t.Error("a spinner tick after the session asked for the next one")
	}
}

func TestUpdateKeepsMessages(t *testing.T) {
	t.Parallel()
	err := errors.New("boom")
	m, cmd := feed(t, New(nil),
		tea.WindowSizeMsg{Width: 100, Height: 30},
		SummaryMsg{Session: sessionReport()},
		DoneMsg{Err: err},
		"an unknown message",
	)
	if cmd != nil {
		t.Error("an unknown message returned a command")
	}
	if m.width != 100 || m.height != 30 {
		t.Errorf("size = %dx%d, want 100x30", m.width, m.height)
	}
	if !m.hasSummary || m.summary.Session.Rows != 8 {
		t.Error("the summary is missing")
	}
	if !m.done || !errors.Is(m.doneErr, err) {
		t.Errorf("done = %v, err = %v, want true and %v", m.done, m.doneErr, err)
	}
}
