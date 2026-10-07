package app

import (
	"context"
	"fmt"

	"dyno/internal/cli"
	"dyno/internal/ui/plain"
)

// Models executes the models command.
func Models(ctx context.Context, cmd cli.ModelsCommand, stdio IO) error {
	client, err := newClient(cmd.Common)
	if err != nil {
		return err
	}
	warnEndpoint(cmd.Common, stdio)
	models, err := client.Models(ctx)
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}
	return plain.WriteModels(stdio.Out, models)
}
