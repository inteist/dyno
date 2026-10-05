package domain

import "fmt"

// Step is one scheduled measurement.
type Step struct {
	Model string
	Mode  CacheMode
	Run   int // 1-based
}

// String returns a short label such as "qwen3.5:4b-mlx miss run 2".
func (s Step) String() string {
	return fmt.Sprintf("%s %s run %d", s.Model, s.Mode, s.Run)
}
