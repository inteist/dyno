package sysinfo

import (
	"context"
	"slices"
	"strings"
	"testing"

	"dyno/internal/domain"
)

func TestShortName(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"Maxs-MacBook-Pro.local": "Maxs-MacBook-Pro",
		"Maxs-MacBook-Pro":       "Maxs-MacBook-Pro",
		"build.example.com":      "build.example.com",
		"":                       "",
	} {
		if got := shortName(in); got != want {
			t.Errorf("shortName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProbe(t *testing.T) {
	t.Parallel()
	host := Probe(context.Background())
	t.Logf("Probe() = %+v", host)
	valid := []string{domain.PowerAC, domain.PowerBattery, domain.PowerUnknown}
	if !slices.Contains(valid, host.PowerSource) {
		t.Errorf("PowerSource = %q, want one of %v", host.PowerSource, valid)
	}
	if host.Name == "" || strings.HasSuffix(host.Name, ".local") {
		t.Errorf("Name = %q, want a short host name", host.Name)
	}
}

func TestProbeWithCanceledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := Probe(ctx).PowerSource; got != domain.PowerUnknown {
		t.Errorf("PowerSource = %q, want unknown", got)
	}
}
