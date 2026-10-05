package sysinfo

import (
	"strings"

	"token-rate/internal/domain"
)

// parsePowerSource reads the power source from the output of `pmset -g batt`,
// whose first line is for example "Now drawing from 'AC Power'".
func parsePowerSource(out string) string {
	_, rest, ok := strings.Cut(out, "drawing from '")
	if !ok {
		return domain.PowerUnknown
	}
	source, _, _ := strings.Cut(rest, "'")
	switch source {
	case "AC Power":
		return domain.PowerAC
	case "Battery Power":
		return domain.PowerBattery
	}
	return domain.PowerUnknown
}
