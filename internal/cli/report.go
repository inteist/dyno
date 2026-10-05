package cli

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"time"

	"token-rate/internal/domain"
	"token-rate/internal/report"
)

// ReportCommand summarizes the stored runs.
type ReportCommand struct {
	Common
	Filter     report.Filter
	Dimensions []report.Dimension
	Plain      bool // a plain table, also in a terminal
}

func (ReportCommand) command() {}

// Days that a date in --since and --until adds to its midnight. A date in
// --until counts in full: the filter ends at the next midnight, because
// Filter.Until is exclusive.
const (
	sinceExtraDays = 0
	untilExtraDays = 1
)

// defineReport registers the flags of the report command.
func defineReport(fs *flag.FlagSet, env Env) builder {
	cmd := &ReportCommand{Dimensions: report.DefaultDimensions()}
	addFilter(fs, &cmd.Filter, env.location())
	fs.Var(listValue(&cmd.Dimensions, report.ParseDimension), "group",
		"comma-separated `dimensions`: "+domain.JoinEnum(report.Dimensions()))
	fs.BoolVar(&cmd.Plain, "plain", false, "a plain table, also in a terminal")
	cmd.addDB(fs, env)
	return func() (Command, error) {
		if err := cmd.checkDB("report"); err != nil {
			return nil, err
		}
		if err := checkReport(*cmd); err != nil {
			return nil, usageErrorf("report: %w", err)
		}
		return *cmd, nil
	}
}

// addFilter registers the flags that select the rows.
func addFilter(fs *flag.FlagSet, f *report.Filter, loc *time.Location) {
	fs.Var(dateValue(&f.Since, loc, sinceExtraDays), "since", "first `day` (YYYY-MM-DD) or time (RFC 3339)")
	fs.Var(dateValue(&f.Until, loc, untilExtraDays), "until",
		"last `day` (YYYY-MM-DD, inclusive) or time (RFC 3339, exclusive)")
	fs.Var(listValue(&f.Models, modelName), "model", "comma-separated model `names`")
	fs.Var(listValue(&f.CacheModes, domain.ParseCacheMode), "cache",
		"comma-separated cache `modes`: "+domain.JoinEnum(domain.CacheModes()))
	fs.Var(listValue(&f.Prompts, asIs[string]), "prompt", "comma-separated prompt `names`")
	fs.Var(listValue(&f.Tags, asIs[string]), "tag", "comma-separated `tags`")
	fs.Var(listValue(&f.Sessions, asIs[domain.SessionID]), "session", "comma-separated session `IDs`")
}

// checkReport returns the first problem of cmd that the flag values cannot
// see alone.
func checkReport(cmd ReportCommand) error {
	if err := checkDimensions(cmd.Dimensions); err != nil {
		return err
	}
	f := cmd.Filter
	if !f.Since.IsZero() && !f.Until.IsZero() && !f.Since.Before(f.Until) {
		return errors.New("--since must be before --until")
	}
	return nil
}

// checkDimensions rejects a dimension that occurs twice, and day together
// with week.
func checkDimensions(dims []report.Dimension) error {
	for i, d := range dims {
		if slices.Contains(dims[:i], d) {
			return fmt.Errorf("--group: the dimension %s occurs more than once", d)
		}
	}
	if slices.Contains(dims, report.ByDay) && slices.Contains(dims, report.ByWeek) {
		return errors.New("--group: use day or week, not both")
	}
	return nil
}
