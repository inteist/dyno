package sysinfo

import (
	"context"
	"os"
	"strings"

	"dyno/internal/domain"
)

// Probe returns the short host name and the power source of this machine.
// A fact that it cannot read is empty or domain.PowerUnknown.
func Probe(ctx context.Context) domain.Host {
	return domain.Host{Name: hostName(), PowerSource: powerSource(ctx)}
}

// hostName returns the short host name, or "" when the system has none.
func hostName() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return shortName(name)
}

// shortName removes the ".local" suffix that macOS adds, as in
// "Maxs-MacBook-Pro.local".
func shortName(name string) string { return strings.TrimSuffix(name, ".local") }
