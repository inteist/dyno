package format

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"token-rate/internal/report"
	"token-rate/internal/stats"
)

// Separator joins the parts of one line, as in "decode 55.4 tok/s · ttft 71 ms".
const Separator = " · "

// Missing stands for a value that does not exist, such as the median of no
// runs.
const Missing = "-"

// Int returns n with thousands separators, for example "2,207".
func Int(n int) string { return grouped(strconv.Itoa(n)) }

// Count returns n and the noun, in the plural unless n is 1, for example
// "27 steps".
func Count(n int, noun string) string {
	if n != 1 {
		noun += "s"
	}
	return Int(n) + " " + noun
}

// Rate returns a rate with one decimal, for example "55.4" tokens per second.
func Rate(perSecond float64) string { return fixed(perSecond, 1) }

// Millis returns milliseconds as a whole number, for example "2,207".
func Millis(ms float64) string { return fixed(ms, 0) }

// Percent returns a share from 0 to 1 as a whole percentage, for example
// "99 %".
func Percent(share float64) string { return fixed(share*100, 0) + " %" }

// Duration returns d in the unit that people read best: whole milliseconds
// below 1 s ("71 ms"), three significant digits below 1 min ("2.24 s",
// "3.3 s", "30 s"), and whole seconds in hours and minutes from 1 min
// ("1 min 30 s"). The rule applies to the rounded value, so 999.96 ms is
// "1 s".
func Duration(d time.Duration) string {
	if d < 0 {
		return "-" + positiveDuration(-d.Seconds())
	}
	return positiveDuration(d.Seconds())
}

func positiveDuration(seconds float64) string {
	switch {
	case math.Round(seconds*1000) < 1000:
		return Millis(seconds*1000) + " ms"
	case math.Round(seconds) < 60:
		return strconv.FormatFloat(seconds, 'g', 3, 64) + " s"
	}
	return clock(int64(math.Round(seconds)))
}

// clock returns whole seconds in hours, minutes, and seconds, for example
// "1 h 5 s". It leaves out the parts that are zero.
func clock(seconds int64) string {
	units := [...]struct {
		size int64
		name string
	}{{3600, "h"}, {60, "min"}, {1, "s"}}
	parts := make([]string, 0, len(units))
	for _, u := range units {
		if n := seconds / u.size; n > 0 {
			parts = append(parts, Int(int(n))+" "+u.name)
		}
		seconds %= u.size
	}
	return strings.Join(parts, " ")
}

// Median returns the median of s through value, or Missing when s has no
// values.
func Median(s stats.Summary, value func(float64) string) string {
	if s.N == 0 {
		return Missing
	}
	return value(s.Median)
}

// Spread returns the median and the standard deviation of s through value,
// for example "56.2 ± 0.8", or Missing when s has no values.
func Spread(s stats.Summary, value func(float64) string) string {
	if s.N == 0 {
		return Missing
	}
	return value(s.Median) + " ± " + value(s.StdDev)
}

// Bytes returns a size in decimal units with one decimal, for example
// "5.6 GB", as `ollama list` counts them.
func Bytes(n int64) string {
	if n > -1000 && n < 1000 {
		return Int(int(n)) + " B"
	}
	v := float64(n)
	for _, unit := range [...]string{"KB", "MB", "GB", "TB", "PB"} {
		v /= 1000
		if math.Round(math.Abs(v)*10) < 10000 {
			return fixed(v, 1) + " " + unit
		}
	}
	return fixed(v/1000, 1) + " EB"
}

// Excluded returns the counts of the flags in the order of report.Flags, for
// example "1 failed, 2 short". It returns "" when no run is excluded.
func Excluded(counts map[report.Flag]int) string {
	var parts []string
	for _, f := range report.Flags() {
		if n := counts[f]; n > 0 {
			parts = append(parts, Int(n)+" "+string(f))
		}
	}
	return strings.Join(parts, ", ")
}

// Date returns the calendar day of t in the location of t, for example
// "2026-10-05".
func Date(t time.Time) string { return t.Format(time.DateOnly) }

// DateRange returns "2026-10-05 → 2026-11-02", or one date when first and
// last fall on the same day.
func DateRange(first, last time.Time) string {
	from, to := Date(first), Date(last)
	if from == to {
		return from
	}
	return from + " → " + to
}

// Totals returns the counts and the dates of r, for example
// "412 runs · 400 valid · 23 sessions · 2026-10-05 → 2026-11-02". It leaves
// out the dates when r has no runs.
func Totals(r report.Report) string {
	parts := []string{Count(r.Rows, "run"), Int(r.Valid) + " valid", Count(r.Sessions, "session")}
	if !r.First.IsZero() {
		parts = append(parts, DateRange(r.First, r.Last))
	}
	return strings.Join(parts, Separator)
}

// Titles of the two reports after a session, in both UIs.
const (
	SessionTitle = "this session"
	AllTimeTitle = "all sessions with this workload"
)

// NoRuns tells that a report has no groups.
const NoRuns = "no runs match"

// Overview returns the Totals of r, or NoRuns when r has no groups.
func Overview(r report.Report) string {
	if len(r.Groups) == 0 {
		return NoRuns
	}
	return Totals(r)
}

// Error returns the message of err on one line, or "" for nil.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return strings.ReplaceAll(err.Error(), "\n", "; ")
}

// Failed returns "failed" and the message of err, for a failed step.
func Failed(err error) string {
	if err == nil {
		return "failed"
	}
	return "failed: " + Error(err)
}

// SessionError returns Error(err) for the error of a session. It returns ""
// when err only tells that the user or a signal stopped the session,
// because the status "aborted" says that already.
func SessionError(err error) string {
	if errors.Is(err, context.Canceled) {
		return ""
	}
	return Error(err)
}

// fixed rounds v half away from zero to the given number of decimals and
// adds thousands separators. It returns Missing for NaN and infinities.
func fixed(v float64, decimals int) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Missing
	}
	scale := math.Pow10(decimals)
	return grouped(strconv.FormatFloat(math.Round(v*scale)/scale, 'f', decimals, 64))
}

// grouped adds thousands separators to a number that strconv wrote, such as
// "-1234.5". It drops the sign of a zero, so that "-0.0" becomes "0.0".
func grouped(number string) string {
	digits, negative := strings.CutPrefix(number, "-")
	whole, fraction, hasFraction := strings.Cut(digits, ".")
	out := groupThousands(whole)
	if hasFraction {
		out += "." + fraction
	}
	if negative && strings.Trim(digits, "0.") != "" {
		out = "-" + out
	}
	return out
}

// groupThousands puts a comma between each group of three digits.
func groupThousands(digits string) string {
	var b strings.Builder
	for i, digit := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return b.String()
}
