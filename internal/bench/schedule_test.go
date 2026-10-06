package bench

import (
	"slices"
	"testing"

	"dyno/internal/domain"
)

func TestSchedule(t *testing.T) {
	t.Parallel()
	miss, prefix := domain.CacheMiss, domain.CachePrefix
	step := func(model string, mode domain.CacheMode, run int) domain.Step {
		return domain.Step{Model: model, Mode: mode, Run: run}
	}
	tests := []struct {
		name   string
		models []string
		runs   int
		modes  []domain.CacheMode
		order  domain.Order
		want   []domain.Step
	}{
		{
			name: "model order, one mode", models: []string{"a", "b"}, runs: 2,
			modes: []domain.CacheMode{miss}, order: domain.OrderModel,
			want: []domain.Step{step("a", miss, 1), step("a", miss, 2), step("b", miss, 1), step("b", miss, 2)},
		},
		{
			name: "round-robin, one mode", models: []string{"a", "b"}, runs: 2,
			modes: []domain.CacheMode{miss}, order: domain.OrderRoundRobin,
			want: []domain.Step{step("a", miss, 1), step("b", miss, 1), step("a", miss, 2), step("b", miss, 2)},
		},
		{
			name: "model order, two modes", models: []string{"a", "b"}, runs: 2,
			modes: []domain.CacheMode{prefix, miss}, order: domain.OrderModel,
			want: []domain.Step{
				step("a", prefix, 1), step("a", miss, 1), step("a", prefix, 2), step("a", miss, 2),
				step("b", prefix, 1), step("b", miss, 1), step("b", prefix, 2), step("b", miss, 2),
			},
		},
		{
			name: "round-robin, two modes", models: []string{"a", "b"}, runs: 1,
			modes: []domain.CacheMode{miss, prefix}, order: domain.OrderRoundRobin,
			want: []domain.Step{step("a", miss, 1), step("a", prefix, 1), step("b", miss, 1), step("b", prefix, 1)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := Schedule(tt.models, tt.runs, tt.modes, tt.order); !slices.Equal(got, tt.want) {
				t.Errorf("Schedule() =\n%v\nwant\n%v", got, tt.want)
			}
		})
	}
}

func TestScheduleSize(t *testing.T) {
	t.Parallel()
	steps := Schedule([]string{"a", "b", "c"}, 3, domain.CacheModes(), domain.OrderModel)
	if len(steps) != 27 {
		t.Errorf("len(Schedule()) = %d, want 27", len(steps))
	}
}
