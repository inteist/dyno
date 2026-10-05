package reportview

import (
	"image/color"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"token-rate/internal/report"
	"token-rate/internal/ui/format"
	"token-rate/internal/ui/tui/theme"
)

// keyPress returns the message of the key that k names, as
// tea.KeyPressMsg.String returns it.
func keyPress(k string) tea.KeyPressMsg {
	special := map[string]tea.KeyPressMsg{
		"up":        {Code: tea.KeyUp},
		"down":      {Code: tea.KeyDown},
		"tab":       {Code: tea.KeyTab},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"esc":       {Code: tea.KeyEscape},
		"ctrl+c":    {Code: 'c', Mod: tea.ModCtrl},
	}
	if msg, ok := special[k]; ok {
		return msg
	}
	return tea.KeyPressMsg{Code: []rune(k)[0], Text: k}
}

func TestSelectionMovesAndClamps(t *testing.T) {
	t.Parallel()
	tests := []struct {
		keys []string
		want int
	}{
		{nil, 0},
		{[]string{"up"}, 0},
		{[]string{"k"}, 0},
		{[]string{"down"}, 1},
		{[]string{"j", "j"}, 2},
		{[]string{"down", "down", "down", "j"}, 2},
		{[]string{"down", "down", "up"}, 1},
		{[]string{"j", "k", "k"}, 0},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			t.Parallel()
			r := sampleReport(8)
			m := press(t, New(r), tt.keys...)
			if got := m.table.Cursor(); got != tt.want {
				t.Fatalf("Cursor() = %d, want %d", got, tt.want)
			}
			if got := m.selectedGroup().Key; got != r.Groups[tt.want].Key {
				t.Errorf("selectedGroup().Key = %+v, want %+v", got, r.Groups[tt.want].Key)
			}
			for i, row := range m.table.Rows() {
				if mark := row[0] == marker; mark != (i == tt.want) {
					t.Errorf("row %d has the marker %v, want %v", i, mark, i == tt.want)
				}
			}
		})
	}
}

func TestSelectionScrollsALongTable(t *testing.T) {
	t.Parallel()
	r := report.Build(sampleRows(8), []report.Dimension{report.ByModel, report.ByMode, report.ByDay}, zone)
	m := New(r)
	if rows := m.table.Height(); rows >= len(r.Groups) {
		t.Fatalf("the table shows %d of %d groups, want fewer", rows, len(r.Groups))
	}
	for range r.Groups {
		m = press(t, m, "down")
	}
	last := r.Groups[len(r.Groups)-1]
	want := marker + fitCell(last.Key.Model, m.fit.widths[0], false)
	if view := screen(m); !strings.Contains(view, want) {
		t.Errorf("the view does not show the selected last group %q:\n%s", want, view)
	}
	assertFits(t, screen(m), m.window)
}

func TestMetricCyclesBothWays(t *testing.T) {
	t.Parallel()
	metrics := report.Metrics()
	last := len(metrics) - 1
	tests := []struct {
		keys []string
		want report.Metric
	}{
		{nil, metrics[0]},
		{[]string{"tab"}, metrics[1]},
		{[]string{"tab", "tab"}, metrics[2]},
		{slices.Repeat([]string{"tab"}, len(metrics)), metrics[0]},
		{[]string{"shift+tab"}, metrics[last]},
		{[]string{"shift+tab", "shift+tab"}, metrics[last-1]},
		{[]string{"tab", "shift+tab"}, metrics[0]},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.keys, ","), func(t *testing.T) {
			t.Parallel()
			m := press(t, New(sampleReport(8)), tt.keys...)
			if got := m.selectedMetric(); got != tt.want {
				t.Errorf("selectedMetric() = %s, want %s", got, tt.want)
			}
			if view := screen(m); !strings.Contains(view, tt.want.Label()+format.Separator+dailyLabel) {
				t.Errorf("the chart heading does not name %s:\n%s", tt.want, view)
			}
		})
	}
}

func TestHelpToggles(t *testing.T) {
	t.Parallel()
	m := New(sampleReport(8))
	shown := press(t, m, "?")
	if !shown.help.ShowAll {
		t.Fatal("help.ShowAll = false after ?, want true")
	}
	if shown.chart.height >= m.chart.height {
		t.Errorf("the chart height is %d with the help, want less than %d", shown.chart.height, m.chart.height)
	}
	if !strings.Contains(screen(shown), "previous metric") {
		t.Error("the view does not show the full help")
	}
	hidden := press(t, shown, "?")
	if hidden.help.ShowAll || hidden.chart != m.chart {
		t.Errorf("after ? ?, ShowAll = %v and chart = %+v, want false and %+v", hidden.help.ShowAll, hidden.chart, m.chart)
	}
}

func TestResizeChangesTheSizes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		window     size
		chart      size
		tableWidth int
		tableRows  int
	}{
		{size{80, 24}, size{79, 12}, 80, 3},
		{size{120, 40}, size{119, 29}, 120, 3},
		{size{100, 30}, size{99, 18}, 100, 3},
	}
	for _, tt := range tests {
		t.Run(tt.window.String(), func(t *testing.T) {
			t.Parallel()
			m := resize(New(sampleReport(8)), tt.window)
			if m.chart != tt.chart {
				t.Errorf("chart = %+v, want %+v", m.chart, tt.chart)
			}
			if got := m.table.Width(); got != tt.tableWidth {
				t.Errorf("table.Width() = %d, want %d", got, tt.tableWidth)
			}
			if got := m.table.Height(); got != tt.tableRows {
				t.Errorf("table.Height() = %d, want %d", got, tt.tableRows)
			}
		})
	}
}

func TestQuitKeys(t *testing.T) {
	t.Parallel()
	reports := map[string]report.Report{"groups": sampleReport(2), "empty": {}}
	for name, r := range reports {
		for _, k := range []string{"q", "esc", "ctrl+c"} {
			t.Run(name+"/"+k, func(t *testing.T) {
				t.Parallel()
				_, cmd := New(r).Update(keyPress(k))
				if cmd == nil {
					t.Fatalf("key %q returned no command", k)
				}
				if msg := cmd(); msg != (tea.QuitMsg{}) {
					t.Errorf("key %q returned %T, want tea.QuitMsg", k, msg)
				}
			})
		}
	}
}

func TestEmptyReportIgnoresTheOtherKeys(t *testing.T) {
	t.Parallel()
	m := press(t, New(report.Report{}), "up", "down", "tab", "shift+tab", "?", "x")
	if m.help.ShowAll || m.metric != 0 {
		t.Errorf("ShowAll = %v and metric = %d, want false and 0", m.help.ShowAll, m.metric)
	}
}

func TestInitRequestsTheBackgroundColor(t *testing.T) {
	t.Parallel()
	cmd := New(sampleReport(2)).Init()
	if cmd == nil {
		t.Fatal("Init() = nil")
	}
	if got, want := cmd(), tea.RequestBackgroundColor(); !reflect.DeepEqual(got, want) {
		t.Errorf("Init()() = %#v, want %#v", got, want)
	}
}

func TestBackgroundColorPicksTheTheme(t *testing.T) {
	t.Parallel()
	m := New(sampleReport(2))
	if m.theme.Accent != theme.New(true).Accent {
		t.Error("the first theme is not the dark theme")
	}
	tests := []struct {
		background color.Color
		dark       bool
	}{
		{color.White, false},
		{color.Black, true},
	}
	for _, tt := range tests {
		next, cmd := m.Update(tea.BackgroundColorMsg{Color: tt.background})
		if cmd != nil {
			t.Errorf("BackgroundColorMsg returned a command")
		}
		if got := next.(Model).theme.Accent; got != theme.New(tt.dark).Accent {
			t.Errorf("the background %v gives the accent %v, want the theme for dark = %v", tt.background, got, tt.dark)
		}
	}
}

func TestUpdateIgnoresOtherMessages(t *testing.T) {
	t.Parallel()
	m := New(sampleReport(2))
	next, cmd := m.Update(tea.FocusMsg{})
	if cmd != nil || next.(Model).table.Cursor() != m.table.Cursor() {
		t.Errorf("Update(FocusMsg) changed the model or returned a command")
	}
}

func TestFitColumns(t *testing.T) {
	t.Parallel()
	cols := []format.Column[string]{
		{Header: "NAME", Cell: func(s string) string { return s }},
		{Header: "ID", Cell: func(s string) string { return s + "-0123456789" }},
		{Header: "N", Right: true, Cell: func(string) string { return "12345" }},
	}
	items := []string{"qwen3.5:4b-mlx"} // widths 14, 25, 5
	tests := []struct {
		width int
		want  columnFit
	}{
		{48, columnFit{widths: []int{14, 25, 5}, gap: wideGap}},
		{46, columnFit{widths: []int{14, 25, 5}, gap: narrowGap}},
		{40, columnFit{widths: []int{14, 19, 5}, gap: narrowGap}},
		{28, columnFit{widths: []int{11, 10, 5}, gap: narrowGap}},
		{10, columnFit{widths: []int{10, 10, 5}, gap: narrowGap}},
	}
	for _, tt := range tests {
		got := fitColumns(cols, items, tt.width)
		if got.gap != tt.want.gap || !slices.Equal(got.widths, tt.want.widths) {
			t.Errorf("fitColumns(width %d) = %+v, want %+v", tt.width, got, tt.want)
		}
	}
}

func TestBarHeights(t *testing.T) {
	t.Parallel()
	tests := []struct {
		values  []float64
		heights []float64
		top     float64
	}{
		{[]float64{5, 5}, []float64{1, 1}, 2},
		{[]float64{10, 17, 24}, []float64{2, 9, 16}, 16},
	}
	for _, tt := range tests {
		heights, top := barHeights(tt.values)
		if !slices.Equal(heights, tt.heights) || top != tt.top {
			t.Errorf("barHeights(%v) = %v, %v, want %v, %v", tt.values, heights, top, tt.heights, tt.top)
		}
	}
}

func TestValueRange(t *testing.T) {
	t.Parallel()
	tests := []struct {
		values []float64
		lo, hi float64
	}{
		{[]float64{50, 60}, 49, 61},
		{[]float64{50}, 45, 55},
		{[]float64{0}, 0, 1},
		{[]float64{0.5, 2.5}, 0.3, 2.7},
	}
	for _, tt := range tests {
		lo, hi := valueRange(tt.values)
		if !near(lo, tt.lo) || !near(hi, tt.hi) {
			t.Errorf("valueRange(%v) = %v, %v, want %v, %v", tt.values, lo, hi, tt.lo, tt.hi)
		}
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }
