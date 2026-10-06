package bench

import "dyno/internal/domain"

// Schedule returns the steps of a session in execution order. Inside one
// model and one run number, the steps take the cache modes in the given
// order, so that slow drift affects every mode in the same way.
//
//   - OrderModel: every run of the first model, then the next model.
//   - OrderRoundRobin: run 1 of every model, then run 2, and so on.
func Schedule(models []string, runs int, modes []domain.CacheMode, order domain.Order) []domain.Step {
	steps := make([]domain.Step, 0, len(models)*runs*len(modes))
	add := func(model string, run int) {
		for _, mode := range modes {
			steps = append(steps, domain.Step{Model: model, Mode: mode, Run: run})
		}
	}
	if order == domain.OrderRoundRobin {
		for run := 1; run <= runs; run++ {
			for _, model := range models {
				add(model, run)
			}
		}
		return steps
	}
	for _, model := range models {
		for run := 1; run <= runs; run++ {
			add(model, run)
		}
	}
	return steps
}
