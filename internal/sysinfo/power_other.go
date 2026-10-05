//go:build !darwin

package sysinfo

import (
	"context"

	"token-rate/internal/domain"
)

// powerSource is unknown outside macOS.
func powerSource(context.Context) string { return domain.PowerUnknown }
