package bench

import (
	"context"
	"errors"
	"fmt"
	"time"

	"dyno/internal/domain"
	"dyno/internal/llm"
)

// exchanger sends chat requests and records them as exchanges. Warm-ups,
// primers, and runs all go through send.
type exchanger struct {
	client    llm.Client
	recorder  Recorder
	clock     Clock
	session   domain.SessionID
	gen       llm.GenerationOptions
	keepAlive time.Duration
	timeout   time.Duration
}

// send sends one chat request and records it. It does not record a request
// that ended because the session ended. A request that exceeds the timeout
// is recorded as failed.
func (x *exchanger) send(ctx context.Context, step domain.Step, model domain.Model, kind domain.ExchangeKind,
	conv domain.Conversation, maxTokens int, progress llm.ProgressFunc) (domain.Exchange, domain.Message, error) {

	gen := x.gen
	gen.MaxTokens = maxTokens
	ex := domain.Exchange{Session: x.session, Step: step, Model: model, Kind: kind, StartedAt: x.clock.Now()}

	res, err := x.chat(ctx, llm.ChatRequest{
		Model: model, Conversation: conv, Options: gen, KeepAlive: x.keepAlive,
	}, progress)
	if ctx.Err() != nil {
		return ex, domain.Message{}, ctx.Err()
	}
	if err != nil {
		ex.Err = err.Error()
	} else {
		ex.Measurement = res.Measurement
	}
	if saveErr := x.recorder.SaveExchange(context.WithoutCancel(ctx), ex); saveErr != nil {
		err = errors.Join(err, fmt.Errorf("save exchange: %w", saveErr))
	}
	return ex, res.Reply, err
}

// chat sends req with the request timeout. Its error names the timeout when
// the request took too long.
func (x *exchanger) chat(ctx context.Context, req llm.ChatRequest, progress llm.ProgressFunc) (llm.ChatResult, error) {
	reqCtx, cancel := context.WithTimeout(ctx, x.timeout)
	defer cancel()
	res, err := x.client.Chat(reqCtx, req, progress)
	if err != nil && errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("no answer within the request timeout %s: %w", x.timeout, err)
	}
	return res, err
}
