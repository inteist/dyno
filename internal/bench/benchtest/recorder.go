package benchtest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"dyno/internal/domain"
)

// SessionRecord is a session as the Recorder keeps it.
type SessionRecord struct {
	Session    domain.Session
	Status     domain.SessionStatus
	FinishedAt time.Time
}

// Recorder is an in-memory bench.Recorder. Each call fails with ctx.Err()
// when ctx is done, like a real database. It is safe for concurrent use.
type Recorder struct {
	mu        sync.Mutex
	sessions  map[domain.SessionID]SessionRecord
	models    []domain.Model
	exchanges []domain.Exchange
	saveErr   error
}

// NewRecorder returns an empty recorder.
func NewRecorder() *Recorder {
	return &Recorder{sessions: map[domain.SessionID]SessionRecord{}}
}

// FailSaveExchange makes every later SaveExchange call return err.
func (r *Recorder) FailSaveExchange(err error) *Recorder { r.saveErr = err; return r }

// StartSession keeps s with the status running.
func (r *Recorder) StartSession(ctx context.Context, s domain.Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s.ID]; ok {
		return fmt.Errorf("session %s exists", s.ID)
	}
	r.sessions[s.ID] = SessionRecord{Session: s, Status: domain.StatusRunning}
	return nil
}

// SaveModel keeps m once for each digest.
func (r *Recorder) SaveModel(ctx context.Context, m domain.Model) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.ContainsFunc(r.models, func(o domain.Model) bool { return o.Digest == m.Digest }) {
		r.models = append(r.models, m)
	}
	return nil
}

// SaveExchange keeps x.
func (r *Recorder) SaveExchange(ctx context.Context, x domain.Exchange) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	if _, ok := r.sessions[x.Session]; !ok {
		return fmt.Errorf("unknown session %s", x.Session)
	}
	r.exchanges = append(r.exchanges, x)
	return nil
}

// FinishSession sets the final status of a session.
func (r *Recorder) FinishSession(ctx context.Context, id domain.SessionID, status domain.SessionStatus, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[id]
	if !ok {
		return fmt.Errorf("unknown session %s", id)
	}
	rec.Status, rec.FinishedAt = status, at
	r.sessions[id] = rec
	return nil
}

// Session returns the record of id.
func (r *Recorder) Session(id domain.SessionID) (SessionRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.sessions[id]
	return rec, ok
}

// Models returns the saved models in order.
func (r *Recorder) Models() []domain.Model {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.models)
}

// Exchanges returns the saved exchanges in order.
func (r *Recorder) Exchanges() []domain.Exchange {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.exchanges)
}

// ExchangesOf returns the saved exchanges of kind in order.
func (r *Recorder) ExchangesOf(kind domain.ExchangeKind) []domain.Exchange {
	var out []domain.Exchange
	for _, x := range r.Exchanges() {
		if x.Kind == kind {
			out = append(out, x)
		}
	}
	return out
}
