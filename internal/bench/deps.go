package bench

import (
	"context"
	"time"

	"dyno/internal/domain"
	"dyno/internal/event"
	"dyno/internal/llm"
	"dyno/internal/prompt"
)

// Recorder stores sessions, models, and exchanges. Each call commits at once.
// The SQLite adapter implements it, and benchtest gives an in-memory fake.
type Recorder interface {
	StartSession(ctx context.Context, s domain.Session) error
	SaveModel(ctx context.Context, m domain.Model) error
	SaveExchange(ctx context.Context, x domain.Exchange) error
	FinishSession(ctx context.Context, id domain.SessionID, status domain.SessionStatus, at time.Time) error
}

// Deps are the collaborators of a Runner.
type Deps struct {
	Client   llm.Client
	Recorder Recorder
	Sink     event.Sink
	Clock    Clock
	IDs      prompt.IDSource
	Meta     Meta
}

// Meta holds the facts about the tool and the host for the session row.
type Meta struct {
	Tool string
	Host domain.Host
}
