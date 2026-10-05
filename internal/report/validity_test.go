package report

import (
	"slices"
	"testing"
	"time"

	"token-rate/internal/domain"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	prefix, cont := inMode(domain.CachePrefix), inMode(domain.CacheContinue)
	tests := []struct {
		name    string
		changes []func(*Row)
		want    []Flag
	}{
		{name: "valid"},
		{name: "failed", changes: []func(*Row){failed}, want: []Flag{FlagFailed}},
		{
			name:    "failed with every other problem",
			changes: []func(*Row){short, cached(500), loaded(2 * time.Second), func(r *Row) { r.Err = "boom" }},
			want:    []Flag{FlagFailed},
		},
		{name: "short", changes: []func(*Row){short}, want: []Flag{FlagShort}},
		{name: "no done reason", changes: []func(*Row){func(r *Row) { r.Measurement.DoneReason = "" }}, want: []Flag{FlagShort}},
		{name: "unexpected hit", changes: []func(*Row){cached(17)}, want: []Flag{FlagUnexpectedHit}},
		{name: "exactly 1 % cached in miss mode", changes: []func(*Row){cached(10)}},
		{name: "more than 1 % but only template tokens", changes: []func(*Row){cached(16)}},
		{name: "template tokens of a short prompt", changes: []func(*Row){prompted(54), cached(5)}},
		{name: "most of a short prompt cached", changes: []func(*Row){prompted(54), cached(40)}, want: []Flag{FlagUnexpectedHit}},
		{name: "low cached share in prefix mode", changes: []func(*Row){prefix}},
		{name: "high cached share in prefix mode", changes: []func(*Row){prefix, cached(750)}},
		{name: "high cached share in continue mode", changes: []func(*Row){cont, cached(990)}},
		{name: "load in run", changes: []func(*Row){loaded(time.Second + time.Millisecond)}, want: []Flag{FlagLoadInRun}},
		{name: "exactly 1 s load", changes: []func(*Row){loaded(time.Second)}},
		{name: "first run after a warm-up on MLX", changes: []func(*Row){loaded(118 * time.Millisecond)}},
		{name: "short and load", changes: []func(*Row){short, loaded(2 * time.Second)}, want: []Flag{FlagShort, FlagLoadInRun}},
		{name: "hit and load", changes: []func(*Row){cached(500), loaded(2 * time.Second)}, want: []Flag{FlagUnexpectedHit, FlagLoadInRun}},
		{
			name:    "every flag but failed",
			changes: []func(*Row){short, cached(500), loaded(2 * time.Second)},
			want:    []Flag{FlagShort, FlagUnexpectedHit, FlagLoadInRun},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Classify(changed(newRow(at(5, 8, 0), 50), tt.changes...)); !slices.Equal(got, tt.want) {
				t.Errorf("Classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func prompted(tokens int) func(*Row) {
	return func(r *Row) { r.Measurement.Server.PromptTokens = tokens }
}
