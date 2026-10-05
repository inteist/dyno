package domain

import "time"

// ExchangeKind tells why the tool sent a request.
type ExchangeKind string

const (
	KindWarmup ExchangeKind = "warmup"
	KindPrimer ExchangeKind = "primer"
	KindRun    ExchangeKind = "run"
)

// Exchange is one chat request and its measured response.
type Exchange struct {
	Session     SessionID
	Step        Step
	Model       Model
	Kind        ExchangeKind
	StartedAt   time.Time
	Measurement Measurement // zero when Err is not empty
	Err         string
}

// Failed reports whether the request returned an error.
func (x Exchange) Failed() bool { return x.Err != "" }
