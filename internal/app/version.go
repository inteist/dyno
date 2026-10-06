package app

import (
	"fmt"

	"dyno/internal/domain"
	"dyno/internal/sysinfo"
)

// Version executes the version command.
func Version(stdio IO) error {
	_, err := fmt.Fprintf(stdio.Out, "%s %s\n", domain.ToolName, sysinfo.ToolVersion())
	return err
}
