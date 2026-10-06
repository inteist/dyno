// Command token-rate measures how fast local models generate tokens through
// Ollama, and keeps every run in a SQLite file.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"dyno/internal/app"
	"dyno/internal/cli"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	stdio := app.StdIO()
	cmd, err := cli.Parse(args, cli.OSEnv(), stdio.Err)
	if err != nil {
		return app.Fail(stdio.Err, err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The first signal stops the session, which then cleans up. A second
	// signal gets the default action and ends the process at once.
	context.AfterFunc(ctx, stop)
	return app.Fail(stdio.Err, app.Execute(ctx, cmd, stdio))
}
