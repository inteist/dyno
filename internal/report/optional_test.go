package report

import (
	"slices"
	"testing"
	"time"

	"dyno/internal/domain"
)

func compatibleRow(t time.Time, usage bool) Row {
	r := newRow(t, 20)
	r.Backend, r.ServerKind, r.Endpoint = domain.BackendOpenAI, "vllm", "https://example.test/v1"
	r.MeasurementProtocol, r.ContextPolicy = domain.ProtocolOpenAIChat, "server-managed"
	r.ServerVersion, r.VersionSource = "", "unknown"
	r.Model = domain.Model{Name: "served", Identity: "session:s1:served", IdentitySource: "session"}
	r.Measurement.Server = domain.ServerMetrics{Available: &domain.MetricAvailability{EvalTokens: usage}, EvalTokens: 20}
	r.Measurement.Client = domain.ClientMetrics{TimeToFirstToken: time.Second, Wall: 2 * time.Second}
	return r
}

func TestOptionalMeasurementsMixedReport(t *testing.T) {
	t.Parallel()
	native := newRow(at(5, 8, 0), 30)
	usage := compatibleRow(at(5, 9, 0), true)
	timing := compatibleRow(at(6, 9, 0), false)
	timing.Measurement.Client.TimeToFirstToken = 0
	r := Build([]Row{timing, native, usage}, nil, nil)
	if r.Valid != 3 || len(r.Groups) != 2 || !slices.Equal(r.Dimensions, []Dimension{ByMeasurement}) {
		t.Fatalf("mixed report = %+v", r)
	}
	var g Group
	for _, candidate := range r.Groups {
		if candidate.Key.Measurement == domain.ProtocolOpenAIChat {
			g = candidate
		}
	}
	if g.Valid != 2 || g.Decode.N != 0 || g.Prefill.N != 0 || g.PrefillRate.N != 0 || g.CachedShare.N != 0 || g.TTFT.N != 1 || g.Latency.N != 2 || g.ClientRate.N != 1 {
		t.Fatalf("optional group = %+v", g)
	}
	if g.Latency.Median != 2000 || g.ClientRate.Median != 10 {
		t.Errorf("client wall and rate = %+v, %+v", g.Latency, g.ClientRate)
	}
	if g.Warnings[WarningCacheUnverified] != 2 || g.Warnings[WarningVersionUnknown] != 2 || g.Warnings[WarningReasoningUnknown] != 2 || len(g.Excluded) != 0 {
		t.Errorf("warnings/exclusions = %v / %v", g.Warnings, g.Excluded)
	}
	for _, m := range Metrics() {
		want := g.Summary(m).N
		if len(g.Recent[m]) != want || len(g.Daily[m]) != want {
			t.Errorf("%s: series should include only %d available rows: %v / %v", m, want, g.Recent[m], g.Daily[m])
		}
	}
}

func TestMetricAvailability(t *testing.T) {
	t.Parallel()
	x := domain.Measurement{Server: domain.ServerMetrics{Available: &domain.MetricAvailability{}}, Client: domain.ClientMetrics{Wall: time.Second}}
	for _, m := range Metrics() {
		if got := m.Available(x); got != (m == MetricLatency) {
			t.Errorf("%s available = %v", m, got)
		}
	}
	x.Server.Available = &domain.MetricAvailability{EvalTokens: true, EvalDuration: true, PromptTokens: true, PromptDuration: true}
	x.Server.EvalDuration, x.Server.PromptDuration = time.Second, time.Second
	if !MetricDecode.Available(x) || !MetricClientRate.Available(x) || !MetricPrefill.Available(x) {
		t.Error("known zero usage must remain a known zero")
	}
	if MetricPrefillRate.Available(x) || CachedShareAvailable(x) {
		t.Error("unknown cached tokens must not imply zero cache")
	}
	x.Server.Available.CachedTokens = true
	if !MetricPrefillRate.Available(x) || !CachedShareAvailable(x) {
		t.Error("known zero cached count should be usable")
	}
	x.Server.Available = nil
	if !MetricDecode.Available(x) || !MetricPrefillRate.Available(x) {
		t.Error("nil availability must retain native legacy semantics")
	}
}

func TestTimingOnlyValidityDoesNotApplyOllamaCalibration(t *testing.T) {
	t.Parallel()
	r := compatibleRow(at(5, 8, 0), false)
	r.Measurement.Server = domain.ServerMetrics{Available: &domain.MetricAvailability{PromptTokens: true, CachedTokens: true, LoadDuration: true}, PromptTokens: 100, CachedTokens: 100, LoadDuration: 10 * time.Second}
	if flags := Classify(r); len(flags) != 0 {
		t.Errorf("compatible cache/load values are not Ollama-calibrated: %v", flags)
	}
	r.Measurement.DoneReason = "stop"
	if flags := Classify(r); !slices.Equal(flags, []Flag{FlagShort}) {
		t.Errorf("short run = %v", flags)
	}
	r = newRow(at(5, 8, 0), 10)
	r.Measurement.Server.Available = &domain.MetricAvailability{}
	r.Measurement.Server.CachedTokens, r.Measurement.Server.LoadDuration = 900, 10*time.Second
	if flags := Classify(r); len(flags) != 0 {
		t.Errorf("absent native inputs must not trigger calibrated exclusions: %v", flags)
	}
}

func TestGroupFullIdentityAndProvenance(t *testing.T) {
	t.Parallel()
	a, b := newRow(at(5, 8, 0), 10), newRow(at(5, 9, 0), 20)
	a.Model.Digest, b.Model.Digest = "0123456789abaaaaaaaa", "0123456789abbbbbbbbb"
	for _, d := range []Dimension{ByDigest, ByModelID} {
		r := Build([]Row{a, b}, []Dimension{d}, nil)
		if len(r.Groups) != 2 || r.Groups[0].Key.Digest != a.Model.StorageKey() || r.Groups[1].Key.Digest != b.Model.StorageKey() {
			t.Errorf("%s groups = %+v", d, r.Groups)
		}
	}
	a, b = compatibleRow(at(5, 8, 0), true), compatibleRow(at(5, 9, 0), true)
	a.Model.Identity, b.Model.Identity = "revision:aaaaaaaaaaaa111", "revision:aaaaaaaaaaaa222"
	r := Build([]Row{a, b}, DefaultDimensions(), nil)
	if len(r.Groups) != 2 {
		t.Fatalf("full compatible identities collapsed: %+v", r.Groups)
	}
	k := r.Groups[0].Key
	if k.Backend != domain.BackendOpenAI || k.Engine != "vllm" || k.Endpoint != a.Endpoint || k.Measurement != a.Protocol() {
		t.Errorf("provenance key = %+v", k)
	}
	b.Model.Identity = a.Model.Identity
	b.Endpoint = "https://other.test/v1"
	if len(Build([]Row{a, b}, DefaultDimensions(), nil).Groups) != 2 {
		t.Error("default grouping merged distinct endpoints")
	}
}

func TestCustomGroupingKeepsProtocolsSeparate(t *testing.T) {
	t.Parallel()
	native := newRow(at(5, 8, 0), 10)
	compatible := compatibleRow(at(5, 9, 0), true)
	compatible.Model.Name = native.Model.Name
	compatible2 := compatible
	compatible2.MeasurementProtocol = "future-chat-v2"
	r := Build([]Row{native, compatible, compatible2}, []Dimension{ByModel}, nil)
	if len(r.Groups) != 3 || !slices.Equal(r.Dimensions, []Dimension{ByModel, ByMeasurement}) {
		t.Fatalf("custom protocol grouping = %+v", r)
	}
	legacy := Build([]Row{native, native}, []Dimension{ByModel}, nil)
	if len(legacy.Groups) != 1 || !slices.Equal(legacy.Dimensions, []Dimension{ByModel}) || legacy.Groups[0].Key.Measurement != "" {
		t.Error("legacy-only custom grouping changed")
	}
}
