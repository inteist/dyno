// Package stats computes descriptive statistics. It imports only the
// standard library.
package stats

import (
	"math"
	"slices"
)

// Summary describes a set of values.
type Summary struct {
	N                              int
	Mean, Median, StdDev, Min, Max float64
}

// Summarize returns the summary of values. StdDev is the sample standard
// deviation. An empty input gives a zero Summary. Summarize does not change
// values.
func Summarize(values []float64) Summary {
	if len(values) == 0 {
		return Summary{}
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	mean := meanOf(sorted)
	return Summary{
		N:      len(sorted),
		Mean:   mean,
		Median: medianOfSorted(sorted),
		StdDev: sampleStdDev(sorted, mean),
		Min:    sorted[0],
		Max:    sorted[len(sorted)-1],
	}
}

// Median returns the median of values, or 0 for an empty input.
func Median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return medianOfSorted(sorted)
}

func meanOf(values []float64) float64 {
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func medianOfSorted(sorted []float64) float64 {
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func sampleStdDev(values []float64, mean float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var squares float64
	for _, v := range values {
		squares += (v - mean) * (v - mean)
	}
	return math.Sqrt(squares / float64(len(values)-1))
}
