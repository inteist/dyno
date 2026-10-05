package app

import (
	"context"

	"token-rate/internal/cli"
	"token-rate/internal/store/sqlite"
)

// Report executes the report command.
func Report(ctx context.Context, cmd cli.ReportCommand, stdio IO) error {
	db, err := openStore(ctx, cmd.Common)
	if err != nil {
		return err
	}
	defer db.Close()
	r, err := buildReport(ctx, sqlite.NewSource(db), cmd.Filter, cmd.Dimensions)
	if err != nil {
		return err
	}
	return chooseReportUI(cmd.Plain, stdio).Show(ctx, r)
}
