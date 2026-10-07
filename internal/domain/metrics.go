package domain

import "time"

// ServerMetrics holds the timings that the server reports for one request.
type ServerMetrics struct {
	// Available is nil for legacy/native measurements (all server fields known).
	// External adapters and SQL readers use an explicit mask to preserve NULL.
	Available      *MetricAvailability
	PromptTokens   int
	CachedTokens   int
	PromptDuration time.Duration
	EvalTokens     int
	EvalDuration   time.Duration
	LoadDuration   time.Duration
	TotalDuration  time.Duration
}

// MetricAvailability records known server values; a known zero differs from absent.
type MetricAvailability struct {
	PromptTokens, CachedTokens, PromptDuration            bool
	EvalTokens, EvalDuration, LoadDuration, TotalDuration bool
}

// ServerMetric identifies a nullable raw server value.
type ServerMetric string

const (
	MetricPromptTokens   ServerMetric = "prompt_tokens"
	MetricCachedTokens   ServerMetric = "cached_tokens"
	MetricPromptDuration ServerMetric = "prompt_ns"
	MetricEvalTokens     ServerMetric = "eval_tokens"
	MetricEvalDuration   ServerMetric = "eval_ns"
	MetricLoadDuration   ServerMetric = "load_ns"
	MetricTotalDuration  ServerMetric = "total_ns"
)

// Has reports whether a raw metric was supplied. Nil preserves legacy literals.
func (m ServerMetrics) Has(metric ServerMetric) bool {
	switch metric {
	case MetricPromptTokens:
		return m.Available == nil || m.Available.PromptTokens
	case MetricCachedTokens:
		return m.Available == nil || m.Available.CachedTokens
	case MetricPromptDuration:
		return m.Available == nil || m.Available.PromptDuration
	case MetricEvalTokens:
		return m.Available == nil || m.Available.EvalTokens
	case MetricEvalDuration:
		return m.Available == nil || m.Available.EvalDuration
	case MetricLoadDuration:
		return m.Available == nil || m.Available.LoadDuration
	case MetricTotalDuration:
		return m.Available == nil || m.Available.TotalDuration
	}
	return false
}

// DecodeRate returns the generated tokens per second.
func (m ServerMetrics) DecodeRate() float64 {
	return perSecond(m.EvalTokens, m.EvalDuration)
}

// PrefillRate returns the evaluated prompt tokens per second. It leaves out
// the cached tokens, like "prompt eval rate" in `ollama run --verbose`.
func (m ServerMetrics) PrefillRate() float64 {
	return perSecond(max(0, m.PromptTokens-m.CachedTokens), m.PromptDuration)
}

// CachedShare returns the part of the prompt that came from the cache, from
// 0 to 1.
func (m ServerMetrics) CachedShare() float64 {
	if m.PromptTokens == 0 {
		return 0
	}
	return float64(m.CachedTokens) / float64(m.PromptTokens)
}

func perSecond(n int, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d.Seconds()
}

// ClientMetrics holds the timings that the client measures for one request.
type ClientMetrics struct {
	TimeToFirstToken time.Duration // zero if no token arrived
	Wall             time.Duration
}

// DoneLength is the done reason of a response that reached num_predict.
const DoneLength = "length"

// Measurement is the full result of one request.
type Measurement struct {
	Server     ServerMetrics
	Client     ClientMetrics
	DoneReason string // DoneLength when the model reached num_predict
}
