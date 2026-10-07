package reportview

import (
	"math"
	"slices"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/NimbleMarkets/ntcharts/v2/linechart"
	"github.com/NimbleMarkets/ntcharts/v2/linechart/timeserieslinechart"
	"github.com/NimbleMarkets/ntcharts/v2/sparkline"

	"dyno/internal/report"
	"dyno/internal/ui/format"
	"dyno/internal/ui/tui/theme"
)

// Texts and scales of the charts.
const (
	dailyLabel        = "daily median"
	oneDayHint        = "one day of data: no trend yet"
	noValidRuns       = "no valid runs for this metric in this group"
	metricUnavailable = "metric unavailable in this group"
	dateStep          = 14  // columns between two date labels; a date takes 10
	valueStep         = 2   // rows between two value labels
	rangeMargin       = 0.1 // the space above and below the line, as a part of its range
	barFloor          = 7   // the lowest bar takes 1/(barFloor+1) of the sparkline height
)

// dailyView returns the line chart of the daily medians, or a hint when
// there are fewer than two days.
func dailyView(th theme.Theme, points []report.Point, metric report.Metric, sz size) string {
	if len(points) < 2 {
		return th.Muted.Render(oneDayHint)
	}
	lo, hi := valueRange(pointValues(points))
	chart := timeserieslinechart.New(sz.width, sz.height,
		timeserieslinechart.WithXLabelFormatter(dateLabel(points[0].Time.Location())),
		timeserieslinechart.WithYLabelFormatter(valueLabel(format.MetricValue(metric))),
		timeserieslinechart.WithAxesStyles(th.Muted, th.Muted),
		timeserieslinechart.WithStyle(th.Active),
		timeserieslinechart.WithXYSteps(dateStep, valueStep),
		timeserieslinechart.WithYRange(lo, hi),
		timeserieslinechart.WithTimeRange(points[0].Time, points[len(points)-1].Time),
		timeserieslinechart.WithTimeSeries(timePoints(points)),
	)
	chart.Draw()
	return chart.View()
}

// recentView returns the label, the sparkline, and the range of the last
// runs, in width cells. It drops the oldest runs that do not fit.
func recentView(th theme.Theme, points []report.Point, metric report.Metric, width int) string {
	value := format.MetricValue(metric)
	values := pointValues(points)
	values = values[len(values)-barsThatFit(values, value, width):]
	return th.Muted.Render(recentLabel(len(values))) + " " +
		sparklineView(th, values) + " " +
		th.Muted.Render(valueSpan(values, value))
}

// barsThatFit returns how many bars fit into width cells between the label
// and the range of values, with a space on each side. It is at least 1.
func barsThatFit(values []float64, value func(float64) string, width int) int {
	text := lipgloss.Width(recentLabel(len(values)) + "  " + valueSpan(values, value))
	return min(len(values), max(1, width-text))
}

// sparklineView draws one bar for each value. The bars start below the
// lowest value, so that small changes show.
func sparklineView(th theme.Theme, values []float64) string {
	heights, top := barHeights(values)
	s := sparkline.New(len(values), 1,
		sparkline.WithStyle(th.Active),
		sparkline.WithNoAutoMaxValue(),
		sparkline.WithMaxValue(top),
		sparkline.WithData(heights),
	)
	s.Draw()
	return s.View()
}

// recentLabel returns the label of the sparkline, for example
// "last 50 runs".
func recentLabel(n int) string { return "last " + format.Count(n, "run") }

// valueSpan returns the lowest and the highest value, for example
// "55.1–57.0".
func valueSpan(values []float64, value func(float64) string) string {
	return value(slices.Min(values)) + "–" + value(slices.Max(values))
}

// valueLabel adapts value to a label formatter of the Y axis.
func valueLabel(value func(float64) string) linechart.LabelFormatter {
	return func(_ int, v float64) string { return value(v) }
}

// dateLabel returns a label formatter of the X axis that shows the day in
// loc. The chart keeps times as seconds since the epoch.
func dateLabel(loc *time.Location) linechart.LabelFormatter {
	return func(_ int, seconds float64) string {
		return format.Date(time.UnixMilli(int64(math.Round(seconds * 1e3))).In(loc))
	}
}

// valueRange returns the Y range of values with a margin above and below.
// One value gets a margin around it. The metrics are never negative, so the
// range does not go below zero.
func valueRange(values []float64) (lo, hi float64) {
	lo, hi = slices.Min(values), slices.Max(values)
	margin := (hi - lo) * rangeMargin
	if margin == 0 {
		margin = max(math.Abs(lo)*rangeMargin, 1)
	}
	return max(0, lo-margin), hi + margin
}

// barHeights returns the heights of the bars of values and the top of their
// scale. Equal values get bars of half the height.
func barHeights(values []float64) (heights []float64, top float64) {
	lo, hi := slices.Min(values), slices.Max(values)
	if lo == hi {
		return slices.Repeat([]float64{1}, len(values)), 2
	}
	floor := lo - (hi-lo)/barFloor
	heights = make([]float64, len(values))
	for i, v := range values {
		heights[i] = v - floor
	}
	return heights, hi - floor
}

// pointValues returns the values of points.
func pointValues(points []report.Point) []float64 {
	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.Value
	}
	return values
}

// timePoints converts points for the line chart.
func timePoints(points []report.Point) []timeserieslinechart.TimePoint {
	out := make([]timeserieslinechart.TimePoint, len(points))
	for i, p := range points {
		out[i] = timeserieslinechart.TimePoint{Time: p.Time, Value: p.Value}
	}
	return out
}
