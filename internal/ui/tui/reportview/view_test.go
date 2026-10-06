package reportview

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"dyno/internal/golden"
	"dyno/internal/report"
)

// windows are the sizes of the golden views.
var windows = []size{{80, 24}, {120, 40}}

func TestViewGolden(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		r    report.Report
		keys []string
	}{
		{"report", sampleReport(8), nil},
		{"report-ttft-third-group", sampleReport(8), []string{"shift+tab", "down", "down"}},
		{"report-help", sampleReport(8), []string{"?"}},
		{"one-day", sampleReport(1), nil},
		{"no-valid-run", failedReport(), nil},
		{"empty", report.Report{Dimensions: report.DefaultDimensions()}, nil},
	}
	for _, tt := range tests {
		for _, w := range windows {
			name := tt.name + "-" + w.String()
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				m := press(t, resize(New(tt.r), w), tt.keys...)
				got := screen(m)
				assertFits(t, got, w)
				golden.Assert(t, name, got)
			})
		}
	}
}

func TestViewUsesTheAlternateScreen(t *testing.T) {
	t.Parallel()
	if !New(sampleReport(2)).View().AltScreen {
		t.Error("View().AltScreen = false, want true")
	}
}

func TestViewSurvivesTinyWindows(t *testing.T) {
	t.Parallel()
	for _, r := range []report.Report{sampleReport(8), sampleReport(1), failedReport(), {}} {
		for _, w := range []size{{0, 0}, {1, 1}, {10, 5}, {30, 10}} {
			m := press(t, resize(New(r), w), "?", "tab", "down")
			for _, line := range strings.Split(screen(m), "\n") {
				if n := ansi.StringWidth(line); n > w.width {
					t.Errorf("a line at %s is %d cells wide: %q", w, n, line)
				}
			}
		}
	}
}

// String returns the size as in "80x24".
func (s size) String() string { return fmt.Sprintf("%dx%d", s.width, s.height) }

// resize sends a window size to m.
func resize(m Model, w size) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: w.width, Height: w.height})
	return next.(Model)
}

// press sends the keys to m and fails on a quit.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		next, cmd := m.Update(keyPress(k))
		if cmd != nil {
			t.Fatalf("key %q returned a command", k)
		}
		m = next.(Model)
	}
	return m
}

// screen returns the view of m without ANSI codes and without the spaces at
// the end of the lines.
func screen(m Model) string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n") + "\n"
}

// assertFits checks that the view has no line wider than the window and no
// more lines than the window.
func assertFits(t *testing.T, view string, w size) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(view, "\n"), "\n")
	if len(lines) > w.height {
		t.Errorf("the view has %d lines, the window %d", len(lines), w.height)
	}
	for i, line := range lines {
		if n := ansi.StringWidth(line); n > w.width {
			t.Errorf("line %d is %d cells wide, the window %d: %q", i+1, n, w.width, line)
		}
	}
}
