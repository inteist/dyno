package sysinfo

import (
	"testing"

	"dyno/internal/domain"
)

func TestParsePowerSource(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		out  string
		want string
	}{
		{"MacBook on AC", "Now drawing from 'AC Power'\n" +
			" -InternalBattery-0 (id=35979363)\t100%; charged; 0:00 remaining present: true\n", domain.PowerAC},
		{"MacBook on battery", "Now drawing from 'Battery Power'\n" +
			" -InternalBattery-0 (id=35979363)\t87%; discharging; 4:31 remaining present: true\n", domain.PowerBattery},
		{"desktop Mac", "Now drawing from 'AC Power'\n", domain.PowerAC},
		{"UPS", "Now drawing from 'UPS Power'\n", domain.PowerUnknown},
		{"empty", "", domain.PowerUnknown},
		{"error text", "pmset: unknown option -- x\n", domain.PowerUnknown},
		{"source in another line", "Battery Power is low\nAC Power\n", domain.PowerUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := parsePowerSource(tt.out); got != tt.want {
				t.Errorf("parsePowerSource() = %q, want %q", got, tt.want)
			}
		})
	}
}
