package app

import (
	"fmt"

	"token-rate/internal/sysinfo"
)

// Version executes the version command.
func Version(stdio IO) error {
	_, err := fmt.Fprintf(stdio.Out, "token-rate %s\n", sysinfo.ToolVersion())
	return err
}
