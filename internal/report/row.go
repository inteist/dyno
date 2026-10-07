package report

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"dyno/internal/domain"
)

// Row is one run with the session facts that the report groups by.
type Row struct {
	Session                                            domain.SessionID
	StartedAt                                          time.Time // the start of the run, not of the session
	Tag                                                string
	ServerVersion                                      string
	Backend, ServerKind, Endpoint, MeasurementProtocol string
	ContextPolicy, ConfigJSON, VersionSource           string
	Workload                                           domain.Workload
	Model                                              domain.Model
	Mode                                               domain.CacheMode
	Measurement                                        domain.Measurement
	Err                                                string
}

// BackendName supplies the historical backend for legacy rows.
func (r Row) BackendName() string { return (domain.Session{Backend: r.Backend}).BackendName() }

// Protocol supplies the historical measurement protocol for legacy rows.
func (r Row) Protocol() string {
	return (domain.Session{Backend: r.Backend, MeasurementProtocol: r.MeasurementProtocol}).Protocol()
}

func (r Row) Native() bool {
	return (domain.Session{Backend: r.Backend, MeasurementProtocol: r.MeasurementProtocol}).Native()
}

// PolicyKey fingerprints comparability settings and context policy.
// An empty key denotes the legacy or equivalent native Ollama policy, not a
// known empty configuration. JSON object order is immaterial; numbers retain
// their precision.
func (r Row) PolicyKey() string {
	if r.Native() && domain.IsNativePolicy(r.ContextPolicy, r.ConfigJSON) {
		return ""
	}
	config := r.ConfigJSON
	if canonical, err := domain.ComparableConfigJSON(config); err == nil {
		config = canonical
	}
	// Keep unknown (empty) and malformed configurations distinct from known
	// JSON, and frame both strings so neither can hide changes in the other.
	policy, _ := json.Marshal(struct {
		ConfigJSON    string
		ContextPolicy string
	}{config, r.ContextPolicy})
	sum := sha256.Sum256(policy)
	return hex.EncodeToString(sum[:])
}

// policyKeys is local to one Build. Many runs share session policy facts; do
// their JSON canonicalization and hashing once, not once per measurement.
// Keys retain the raw configuration so malformed or different facts never alias.
type policyFacts struct {
	backend, protocol, context, config string
}
type policyKeys map[policyFacts]string

func (keys policyKeys) key(r Row) string {
	facts := policyFacts{r.BackendName(), r.Protocol(), r.ContextPolicy, r.ConfigJSON}
	if key, ok := keys[facts]; ok {
		return key
	}
	key := r.PolicyKey()
	keys[facts] = key
	return key
}

// Engine returns the declared engine, with truthful defaults for old rows.
func (r Row) Engine() string {
	if r.ServerKind != "" {
		return r.ServerKind
	}
	if r.BackendName() == domain.BackendOllama {
		return domain.BackendOllama
	}
	return "unknown"
}

// Source loads rows. The SQLite adapter implements it.
type Source interface {
	// Rows returns the runs that f selects, in the order of StartedAt.
	// Warm-ups and primers are never rows.
	Rows(ctx context.Context, f Filter) ([]Row, error)
}
