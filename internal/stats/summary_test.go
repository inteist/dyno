package stats

import (
	"math"
	"testing"
)

func TestSummarize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []float64
		want   Summary
	}{
		{name: "empty", values: nil, want: Summary{}},
		{name: "one value", values: []float64{7}, want: Summary{N: 1, Mean: 7, Median: 7, Min: 7, Max: 7}},
		{name: "odd count", values: []float64{3, 1, 2}, want: Summary{N: 3, Mean: 2, Median: 2, StdDev: 1, Min: 1, Max: 3}},
		{name: "even count", values: []float64{4, 1, 3, 2}, want: Summary{N: 4, Mean: 2.5, Median: 2.5, StdDev: math.Sqrt(5.0 / 3), Min: 1, Max: 4}},
		{name: "known deviation", values: []float64{2, 4, 4, 4, 5, 5, 7, 9}, want: Summary{N: 8, Mean: 5, Median: 4.5, StdDev: math.Sqrt(32.0 / 7), Min: 2, Max: 9}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Summarize(tt.values)
			if got.N != tt.want.N || !near(got.Mean, tt.want.Mean) || !near(got.Median, tt.want.Median) ||
				!near(got.StdDev, tt.want.StdDev) || got.Min != tt.want.Min || got.Max != tt.want.Max {
				t.Errorf("Summarize(%v) = %+v, want %+v", tt.values, got, tt.want)
			}
			if m := Median(tt.values); !near(m, tt.want.Median) {
				t.Errorf("Median(%v) = %v, want %v", tt.values, m, tt.want.Median)
			}
		})
	}
}

func TestSummarizeKeepsInput(t *testing.T) {
	t.Parallel()
	values := []float64{3, 1, 2}
	Summarize(values)
	Median(values)
	if values[0] != 3 || values[1] != 1 || values[2] != 2 {
		t.Errorf("the input changed to %v", values)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
