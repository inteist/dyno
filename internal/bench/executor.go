package bench

import (
	"context"

	"dyno/internal/domain"
	"dyno/internal/event"
)

// stepExecutor implements scenario.Executor for one step and one model.
type stepExecutor struct {
	life     *lifecycle
	exchange *exchanger
	sink     event.Sink
	step     domain.Step
	model    domain.Model
	primers  int // the number of primers of the scenario
	sent     int // the number of primers so far
}

// ResetCache reloads and warms a managed model, or invokes a real external reset.
func (e *stepExecutor) ResetCache(ctx context.Context) error {
	return e.life.resetCache(ctx, e.step, e.model)
}

// Prime announces the primer and sends it as an exchange of kind primer.
func (e *stepExecutor) Prime(ctx context.Context, conv domain.Conversation, maxTokens int) (domain.Message, error) {
	e.sent++
	e.sink.Publish(event.Preparing{Step: e.step, Action: event.ActionPrimer, Index: e.sent, Total: e.primers})
	_, reply, err := e.exchange.send(ctx, e.step, e.model, domain.KindPrimer, conv, maxTokens, nil)
	return reply, err
}
