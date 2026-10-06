//go:build !darwin

package sysinfo

import (
	"context"

	"dyno/internal/domain"
)

// powerSource is unknown outside macOS.
func powerSource(context.Context) string { return domain.PowerUnknown }
