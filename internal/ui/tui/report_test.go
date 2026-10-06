package tui

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"dyno/internal/domain"
	"dyno/internal/report"
)

// reportUI copies the ReportUI port of the app package.
type reportUI interface {
	Show(ctx context.Context, r report.Report) error
}

var _ reportUI = (*ReportUI)(nil)

func TestReportUIShowReturnsNilAfterAQuitKey(t *testing.T) {
	t.Parallel()
	keys := map[string]string{"q": "q", "ctrl+c": "\x03"}
	for name, in := range keys {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out := &syncWriter{}
			if err := show(t.Context(), t, testReportUI(strings.NewReader(in), out)); err != nil {
				t.Fatalf("Show() = %v, want nil", err)
			}
			if out.Len() == 0 {
				t.Error("Show() drew nothing")
			}
		})
	}
}

func TestReportUIShowReturnsTheContextErrorAfterACancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	out := &syncWriter{onWrite: cancel} // the program runs once it draws
	err := show(ctx, t, testReportUI(strings.NewReader(""), out))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Show() = %v, want %v", err, context.Canceled)
	}
}

func TestReportUIShowReturnsTheErrorOfTheProgram(t *testing.T) {
	t.Parallel()
	err := show(t.Context(), t, testReportUI(failingReader{}, &syncWriter{}))
	if !errors.Is(err, errBrokenInput) {
		t.Errorf("Show() = %v, want %v", err, errBrokenInput)
	}
}

// testReportUI returns a ReportUI with a fixed window size, which needs no
// terminal.
func testReportUI(in io.Reader, out io.Writer) *ReportUI {
	u := NewReportUI(in, out)
	u.options = []tea.ProgramOption{tea.WithWindowSize(80, 24)}
	return u
}

// show runs u.Show of a small report and fails the test after showTimeout.
func show(ctx context.Context, t *testing.T, u *ReportUI) error {
	t.Helper()
	return within(t, func() error { return u.Show(ctx, testReport()) })
}

// testReport has one group with a run on each of two days.
func testReport() report.Report {
	at := time.Date(2026, 10, 5, 21, 0, 0, 0, time.UTC)
	first := report.Row{
		Session:       domain.NewSessionID(at, "K3F9A2"),
		StartedAt:     at,
		ServerVersion: "0.34.0",
		Model:         domain.Model{Name: "qwen3.5:4b-mlx", Digest: "a80c4f17acd5"},
		Mode:          domain.CacheMiss,
		Measurement: domain.Measurement{
			Server: domain.ServerMetrics{
				PromptTokens: 2048, PromptDuration: 2 * time.Second, EvalTokens: 512, EvalDuration: 9 * time.Second,
			},
			Client:     domain.ClientMetrics{TimeToFirstToken: 2 * time.Second},
			DoneReason: domain.DoneLength,
		},
	}
	second := first
	second.StartedAt = at.Add(24 * time.Hour)
	return report.Build([]report.Row{first, second}, report.DefaultDimensions(), time.UTC)
}
