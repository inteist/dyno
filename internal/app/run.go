package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"dyno/internal/bench"
	"dyno/internal/cli"
	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/llm"
	"dyno/internal/prompt"
	"dyno/internal/report"
	"dyno/internal/store/sqlite"
	"dyno/internal/sysinfo"
	"dyno/internal/ui/plain"
)

// Run executes the run command.
func Run(ctx context.Context, cmd cli.RunCommand, stdio IO) error {
	client, err := newClient(cmd.Common)
	if err != nil {
		return err
	}
	warnEndpoint(cmd.Common, stdio)
	return runSession(ctx, cmd, stdio, client)
}

// runSession executes a session with client. Tests call it with a fake
// client.
func runSession(ctx context.Context, cmd cli.RunCommand, stdio IO, client llm.Client) error {
	// Canonicalize a copy for both the runner and historical summary filters.
	cmd.Options.Models = llm.NormalizeModelNames(client, cmd.Options.Models)
	db, err := openStore(ctx, cmd.Common)
	if err != nil {
		return err
	}
	defer db.Close()
	logFile, err := openLog(cmd.Common)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}

	ui := chooseSessionUI(cmd.Plain, stdio)
	runner, err := bench.NewRunner(bench.Deps{
		Client:   client,
		Recorder: sqlite.NewRecorder(db),
		Sink:     withLog(ui.Sink(), logFile),
		Clock:    bench.SystemClock{},
		IDs:      prompt.RandomIDs{},
		Meta:     bench.Meta{Tool: sysinfo.ToolVersion(), Host: sysinfo.Probe(ctx)},
	}, cmd.Options)
	if err != nil {
		return err
	}
	source := sqlite.NewSource(db)
	return ui.Run(ctx, func(ctx context.Context) error {
		id, runErr := runner.Run(ctx)
		if id == "" {
			return runErr
		}
		filter := report.Filter{Backends: []string{cmd.BackendName()}}
		if _, managed := client.(llm.ManagedLifecycle); !managed {
			filter.Backends = []string{"unknown"}
			filter.Endpoints = []string{cmd.BaseURL}
			// Use adapter provenance, not the raw flag spelling: trailing slashes
			// and programmatic default URLs must select the stored endpoint.
			if provider, ok := client.(llm.InfoProvider); ok {
				info, err := provider.Info(context.WithoutCancel(ctx))
				if err != nil {
					return errors.Join(runErr, fmt.Errorf("read endpoint provenance: %w", err))
				}
				if info.Backend != "" {
					filter.Backends = []string{info.Backend}
				}
				filter.Endpoints = []string{info.Endpoint}
			}
		}
		if err := showSummary(context.WithoutCancel(ctx), ui, source, id, cmd.Options, filter); err != nil {
			runErr = errors.Join(runErr, err)
		}
		return runErr
	})
}

// withLog adds a plain-text copy of the events to w. It returns sink
// unchanged when w is nil.
func withLog(sink event.Sink, w io.Writer) event.Sink {
	if w == nil {
		return sink
	}
	return event.Tee(sink, plain.NewSessionUI(w, time.Now).Sink())
}

// sessionDimensions group the runs of one session. One session has one
// digest and one server version for each model.
func sessionDimensions() []report.Dimension {
	return []report.Dimension{report.ByModel, report.ByMode}
}

// allTimeDimensions group the runs of all sessions. They keep different
// digests and server versions apart.
func allTimeDimensions() []report.Dimension {
	return report.AllTimeDimensions()
}

// showSummary presents the runs of the session next to all runs with the
// same models, cache modes, and workload.
func showSummary(ctx context.Context, ui SessionUI, src report.Source, id domain.SessionID, o bench.Options, provenance report.Filter) error {
	session, err := buildReport(ctx, src, report.Filter{Sessions: []domain.SessionID{id}}, sessionDimensions())
	if err != nil {
		return err
	}
	f := report.Filter{
		Models: o.Models, CacheModes: o.CacheModes,
		Workloads: []string{o.Workload().Hash()},
	}
	f.Backends, f.Endpoints = provenance.Backends, provenance.Endpoints
	allTime, err := buildReport(ctx, src, f, allTimeDimensions())
	if err != nil {
		return err
	}
	ui.ShowSummary(session, allTime)
	return nil
}

// buildReport loads the rows that f selects and summarizes them by dims.
func buildReport(ctx context.Context, src report.Source, f report.Filter, dims []report.Dimension) (report.Report, error) {
	rows, err := src.Rows(ctx, f)
	if err != nil {
		return report.Report{}, fmt.Errorf("load runs: %w", err)
	}
	return report.Build(rows, dims, time.Local), nil
}
