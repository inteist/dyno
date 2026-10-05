package sysinfo

import (
	"context"
	"os/exec"
	"time"

	"token-rate/internal/domain"
)

// pmsetTimeout limits the pmset call, so that a hung tool cannot delay a
// session.
const pmsetTimeout = 2 * time.Second

// pmsetPath is absolute, because launchd starts the agent with a short PATH.
const pmsetPath = "/usr/bin/pmset"

// powerSource asks pmset for the power source.
func powerSource(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, pmsetTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, pmsetPath, "-g", "batt").Output()
	if err != nil {
		return domain.PowerUnknown
	}
	return parsePowerSource(string(out))
}
