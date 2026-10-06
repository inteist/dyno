package runview

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/golden"
	"dyno/internal/report"
	"dyno/internal/ui/format"
	"dyno/internal/ui/tui/theme"
)

// scenarios are the frames of the golden files.
func scenarios() map[string][]tea.Msg {
	summary := append(events(sessionEvents()),
		SummaryMsg{Session: sessionReport(), AllTime: allTimeReport()},
		DoneMsg{Err: errSteps},
	)
	return map[string][]tea.Msg{
		"running": events(runningEvents()),
		"waiting": append(events(waitingEvents()), tickMsg(waitUntil(4).Add(-17400*time.Millisecond))),
		"summary": summary,
	}
}

func TestViewGolden(t *testing.T) {
	t.Parallel()
	sizes := []struct {
		name          string
		width, height int
	}{{"80x24", 80, 24}, {"120x40", 120, 40}}
	for name, msgs := range scenarios() {
		for _, size := range sizes {
			t.Run(name+"-"+size.name, func(t *testing.T) {
				t.Parallel()
				m := settle(t, feedAll(t, sized(t, size.width, size.height), msgs...))
				assertFits(t, plain(m), size.width, size.height)
				assertWidth(t, seen(m), size.width)
				golden.Assert(t, name+"-"+size.name, seen(m)+"\n")
			})
		}
	}
}

// assertFits fails when frame is wider or taller than the window.
func assertFits(t *testing.T, frame string, width, height int) {
	t.Helper()
	if n := lineCount(frame); n > height {
		t.Errorf("the frame has %d lines, want at most %d", n, height)
	}
	assertWidth(t, frame, width)
}

// assertWidth fails when a line of text is wider than width.
func assertWidth(t *testing.T, text string, width int) {
	t.Helper()
	for _, l := range strings.Split(text, "\n") {
		if w := len([]rune(l)); w > width {
			t.Errorf("line %q has %d columns, want at most %d", l, w, width)
		}
	}
}

func TestViewShortensTheLastRunColumn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		width   int
		want    string
		without []string
	}{
		{80, "prefix: 56.1 tok/s · ttft 318 ms", nil},
		{70, "56.1 tok/s · ttft 318 ms", []string{"prefix: 56.1"}},
		{60, "56.1 tok/s", []string{"ttft 318 ms"}},
		{50, "MODEL", []string{"LAST RUN", "56.1 tok/s"}},
	}
	for _, tt := range tests {
		m, _ := feed(t, sized(t, tt.width, 24), events(runningEvents())...)
		got := plain(m)
		assertFits(t, got, tt.width, 24)
		if !strings.Contains(got, tt.want) {
			t.Errorf("width %d: the frame lacks %q:\n%s", tt.width, tt.want, got)
		}
		for _, w := range tt.without {
			if strings.Contains(got, w) {
				t.Errorf("width %d: the frame has %q:\n%s", tt.width, w, got)
			}
		}
	}
}

// TestTallFinalFrameGoesIntoTheHistory ends a session of 3 models and 3
// cache modes at 80x24. Its final frame is taller than the window, and
// Bubble Tea would cut the top lines of a live frame of that height.
func TestTallFinalFrameGoesIntoTheHistory(t *testing.T) {
	t.Parallel()
	models := []string{qwen, lfm, "qwen3.5:9b-mlx"}
	modes := domain.CacheModes()
	var steps []domain.Step
	r := report.Report{Dimensions: []report.Dimension{report.ByModel, report.ByMode}, Rows: 9, Valid: 9, Sessions: 1, First: start, Last: start}
	for _, model := range models {
		for _, mode := range modes {
			steps = append(steps, step(model, mode, 1))
			r.Groups = append(r.Groups, report.Group{
				Key: report.Key{Model: model, Mode: string(mode)}, Decode: spreadOf(1, 56, 0),
				Prefill: median(1, 300), TTFT: median(1, 320), CachedShare: median(1, 0.5),
			})
		}
	}
	session := sampleSession()
	session.CacheModes = modes
	msgs := events([]event.Event{
		event.SessionStarted{Session: session, Steps: steps},
		event.SessionEnded{ID: session.ID, Status: domain.StatusCompleted},
	})
	m, cmd := feed(t, sized(t, 80, 24), append(msgs, SummaryMsg{Session: r, AllTime: r}, DoneMsg{})...)
	if live := plain(m); live != printingFrame {
		t.Errorf("the live frame during the print = %q, want one blank line", live)
	}

	history := ansi.Strip(m.finalFrame())
	if lines := strings.Count(history, "\n") + 1; lines <= 24 {
		t.Fatalf("the final frame has %d lines; the test needs more than 24", lines)
	}
	for _, want := range []string{"dyno · session", "MODEL", "session " + string(session.ID) + " completed", format.SessionTitle, format.AllTimeTitle} {
		if !strings.Contains(history, want) {
			t.Errorf("the final frame lacks %q:\n%s", want, history)
		}
	}
	if cmd == nil || isQuit(cmd) {
		t.Error("DoneMsg must print the final frame and wait for a key")
	}
	m, _ = feed(t, m, printedMsg{})
	if live := plain(m); strings.Contains(live, "MODEL") || !strings.Contains(live, "q quit") {
		t.Errorf("the live frame after the session = %q, want only the key hint", live)
	}
}

func TestViewLines(t *testing.T) {
	t.Parallel()
	steps := sessionSteps()
	begin := []event.Event{event.SessionStarted{Session: sampleSession(), Steps: steps}, started(0)}
	noLimit := sampleSession()
	noLimit.Workload.NumPredict = 0
	noLimit.Tag, noLimit.Interval = "", 0
	tests := []struct {
		name string
		msgs []tea.Msg
		want []string
	}{
		{"before the session", nil, []string{"dyno\n", "starting the session", "q stop"}},
		{"step started", events(begin), []string{"qwen3.5:4b-mlx · miss · run 1 · starting", "step 1/8"}},
		{"loading", events(append(begin, event.ModelLoading{Model: qwen})), []string{"run 1 · loading the model"}},
		{"ready", events(append(begin, event.ModelReady{Model: qwen})), []string{"run 1 · starting"}},
		{"warm-up", events(append(begin, event.Preparing{Step: steps[0], Action: event.ActionWarmup})), []string{"run 1 · warm-up"}},
		{"reload", events(append(begin, event.Preparing{Step: steps[0], Action: event.ActionReload})), []string{"run 1 · reload"}},
		{"one primer", events(append(begin, event.Preparing{Step: steps[0], Action: event.ActionPrimer, Index: 1, Total: 1})), []string{"run 1 · primer\n"}},
		{"first token", events(append(begin, event.Progress{Step: steps[0], Tokens: 1, Elapsed: time.Second})), []string{"run 1 · 1/512 tokens\n"}},
		{
			"no token limit",
			events([]event.Event{
				event.SessionStarted{Session: noLimit, Steps: steps}, started(0),
				event.Progress{Step: steps[0], Tokens: 1, Elapsed: time.Second},
				event.Progress{Step: steps[0], Tokens: 51, Elapsed: 2 * time.Second},
			}),
			[]string{"session 20261005T120212Z-K3F9A2 · prompt long · 2 runs\n", "run 1 · 51 tokens · 50.0 tok/s"},
		},
		{"finished", events(append(begin, finished(steps[0], measurement(9242*time.Millisecond, 0, 2236*time.Millisecond, 0)))), []string{"✓ qwen3.5:4b-mlx · miss · run 1 · 55.4 tok/s · ttft 2.24 s"}},
		{"failed", events(append(begin, event.StepFailed{Step: steps[0], Err: errEOF})), []string{
			"✗ qwen3.5:4b-mlx · miss · run 1 · failed: chat qwen3.5:4b-mlx: unexpected EOF",
			"1 failed step · last error: chat qwen3.5:4b-mlx: unexpected EOF",
		}},
		{"failed without an error", events(append(begin, event.StepFailed{Step: steps[0]})), []string{"run 1 · failed\n 1 failed step\n"}},
		{"wait without a tick", events(append(begin, waiting(0))), []string{"· next qwen3.5:4b-mlx · prefix · run 1", "step 1/8 · next run in 30 s"}},
		{"wait at its end", append(events(append(begin, waiting(0))), tickMsg(waitUntil(0).Add(time.Second))), []string{"next run now"}},
		{"wait after the last step", events(append(begin, event.StepStarted{Index: 7, Total: 8, Step: steps[7]}, waiting(7))), []string{"· waiting\n"}},
		{"session ended", events(append(begin, event.SessionEnded{ID: "S", Status: domain.StatusAborted, Err: errors.New("stopped\nunload failed")})), []string{
			"session S aborted · 0 failed steps", "error: stopped; unload failed", "q stop",
		}},
		{"did not start", []tea.Msg{DoneMsg{Err: errors.New("list models: refused")}, printedMsg{}}, []string{
			"the session did not start", "error: list models: refused", "q quit",
		}},
		{"done without an end", append(events(begin), DoneMsg{}, printedMsg{}), []string{"0/2\n q quit"}},
		{"empty summary", append(events(begin), SummaryMsg{}, DoneMsg{}, printedMsg{}), []string{"this session · no runs match", "all sessions with this workload · no runs match"}},
		{"question", []tea.Msg{press("q")}, []string{"press q again to stop"}},
		{"stopping", []tea.Msg{press("q"), press("q")}, []string{"stopping · finished runs are saved"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m, _ := feed(t, New(nil), tt.msgs...)
			got := seen(m) + "\n"
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("the frame lacks %q:\n%s", want, got)
				}
			}
		})
	}
}

func TestViewLeavesOutTheHintWhenItQuits(t *testing.T) {
	t.Parallel()
	m, _ := feed(t, New(nil), DoneMsg{}, printedMsg{}, press("q"))
	if got := plain(m); strings.Contains(got, "quit") {
		t.Errorf("the last frame has the key hint:\n%s", got)
	}
}

func TestViewUsesTheThemeOfTheBackground(t *testing.T) {
	t.Parallel()
	dark, _ := feed(t, New(nil), events(runningEvents())...)
	light, _ := feed(t, dark, tea.BackgroundColorMsg{Color: whiteColor{}})
	if dark.View().Content == light.View().Content {
		t.Error("a light background renders the same colors as a dark one")
	}
	if plain(dark) != plain(light) {
		t.Error("the theme changed the text")
	}
}

func TestStatusStyle(t *testing.T) {
	t.Parallel()
	th := theme.New(true)
	tests := []struct {
		e    event.SessionEnded
		want string
	}{
		{event.SessionEnded{Status: domain.StatusCompleted}, th.Good.Render("x")},
		{event.SessionEnded{Status: domain.StatusCompleted, Failed: 1}, th.Warn.Render("x")},
		{event.SessionEnded{Status: domain.StatusAborted}, th.Warn.Render("x")},
		{event.SessionEnded{Status: domain.StatusFailed}, th.Bad.Render("x")},
	}
	for _, tt := range tests {
		if got := statusStyle(th, tt.e).Render("x"); got != tt.want {
			t.Errorf("statusStyle(%s, %d failed) renders %q, want %q", tt.e.Status, tt.e.Failed, got, tt.want)
		}
	}
}

func TestDropOrder(t *testing.T) {
	t.Parallel()
	all := len(allTimeReport().Dimensions)
	tests := []struct {
		dims, cols int
		want       []int
	}{
		{0, 6, []int{5}},
		{1, 7, []int{6}},
		{2, 8, []int{7}},
		{all, 10, []int{1, 2, 9}},
	}
	for _, tt := range tests {
		got := dropOrder(tt.dims, tt.cols)
		if len(got) != len(tt.want) {
			t.Errorf("dropOrder(%d, %d) = %v, want %v", tt.dims, tt.cols, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("dropOrder(%d, %d) = %v, want %v", tt.dims, tt.cols, got, tt.want)
				break
			}
		}
	}
}

func TestReportLinesKeepColumnsThatFit(t *testing.T) {
	t.Parallel()
	m := New(nil)
	r := allTimeReport()
	wide := m.reportLines(format.AllTimeTitle, r, 200)
	narrow := m.reportLines(format.AllTimeTitle, r, 20)
	dropped := []string{report.ByDigest.Header(), report.ByServer.Header(), "EXCLUDED"}
	for _, header := range dropped {
		if !strings.Contains(wide[1], header) {
			t.Errorf("a wide table lacks %s: %q", header, wide[1])
		}
	}
	for _, header := range dropped {
		if strings.Contains(narrow[1], header) {
			t.Errorf("a narrow table keeps %s: %q", header, narrow[1])
		}
	}
	if !strings.Contains(narrow[1], "CACHED") {
		t.Errorf("a narrow table lost the CACHED column: %q", narrow[1])
	}
	if got := m.reportLines(format.SessionTitle, report.Report{}, 80); len(got) != 1 {
		t.Errorf("an empty report has %d lines, want 1", len(got))
	}
}

// whiteColor is the background of a light terminal.
type whiteColor struct{}

func (whiteColor) RGBA() (r, g, b, a uint32) { return 0xffff, 0xffff, 0xffff, 0xffff }

// TestShortFinalFrameStaysLive ends a small session in a tall window. The
// final frame fits, so it stays the live frame and nothing is printed.
func TestShortFinalFrameStaysLive(t *testing.T) {
	t.Parallel()
	m, cmd := feed(t, sized(t, 120, 60), scenarios()["summary"]...)
	if cmd != nil {
		t.Errorf("DoneMsg returned a command for a final frame that fits")
	}
	live := plain(m)
	for _, want := range []string{"dyno · session", "MODEL", format.SessionTitle, format.AllTimeTitle, "q quit"} {
		if !strings.Contains(live, want) {
			t.Errorf("the live frame lacks %q:\n%s", want, live)
		}
	}
	if m, cmd := feed(t, m, press("q")); !isQuit(cmd) || strings.Contains(plain(m), "q quit") {
		t.Errorf("q after a live final frame: quit = %v, frame:\n%s", isQuit(cmd), plain(m))
	}
}

// summaryHeight returns the height of the live final frame of the summary
// scenario at width 80.
func summaryHeight(t *testing.T) int {
	t.Helper()
	return lineCount(plain(feedAll(t, sized(t, 80, 200), scenarios()["summary"]...)))
}

func TestFinalFrameFitBoundary(t *testing.T) {
	t.Parallel()
	h := summaryHeight(t)
	fits, cmd := feed(t, sized(t, 80, h), scenarios()["summary"]...)
	if cmd != nil || fits.inHistory {
		t.Errorf("at %d rows the final frame must stay live", h)
	}
	assertFits(t, plain(fits), 80, h)

	tall, cmd := feed(t, sized(t, 80, h-1), scenarios()["summary"]...)
	if cmd == nil || plain(tall) != printingFrame {
		t.Fatalf("at %d rows the final frame must go into the history", h-1)
	}
	tall = settle(t, tall)
	if live := plain(tall); live != " q quit" {
		t.Errorf("the live frame after the print = %q, want only the key hint", live)
	}
	assertFits(t, plain(tall), 80, h-1)
}

func TestResizeAfterTheEnd(t *testing.T) {
	t.Parallel()
	h := summaryHeight(t)
	m := feedAll(t, sized(t, 80, h), scenarios()["summary"]...)

	taller, cmd := feed(t, m, tea.WindowSizeMsg{Width: 80, Height: h + 10})
	if cmd != nil || taller.inHistory {
		t.Error("a taller window must keep the live final frame")
	}
	smaller, cmd := feed(t, m, tea.WindowSizeMsg{Width: 80, Height: h - 1})
	if cmd == nil || plain(smaller) != printingFrame {
		t.Fatal("a smaller window must print the final frame into the history")
	}
	if smaller = settle(t, smaller); !smaller.inHistory || !strings.Contains(plain(smaller), "q quit") {
		t.Errorf("after the print: inHistory = %v, frame %q", smaller.inHistory, plain(smaller))
	}
}

// TestQuitKeepsTheLastLine checks the last frame after q. Bubble Tea erases
// the last line of an inline frame on exit, and a frame that changes its
// height leaves its old lines in the history. Thus the key hint of a live
// final frame becomes an empty line.
func TestQuitKeepsTheLastLine(t *testing.T) {
	t.Parallel()
	live := feedAll(t, sized(t, 80, summaryHeight(t)), scenarios()["summary"]...)
	last := feedAll(t, live, press("q"))
	frame := plain(last)
	if lineCount(frame) != lineCount(plain(live)) || !strings.HasSuffix(frame, "\n") || strings.Contains(frame, "q quit") {
		t.Errorf("the last frame must keep the height and end with an empty line, without the hint:\n%q", frame)
	}
	if lines := strings.Split(strings.TrimRight(frame, "\n"), "\n"); !strings.Contains(lines[len(lines)-1], "lfm2.5:8b") {
		t.Errorf("the last table row is missing: %q", lines[len(lines)-1])
	}
	printed := feedAll(t, settle(t, feedAll(t, sized(t, 80, 10), scenarios()["summary"]...)), press("q"))
	if frame := plain(printed); frame != "" {
		t.Errorf("after a print, the last frame = %q, want it empty", frame)
	}
}
