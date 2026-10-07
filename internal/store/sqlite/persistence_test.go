package sqlite

import (
	"database/sql"
	"reflect"
	"testing"
	"time"

	"dyno/internal/domain"
	"dyno/internal/report"
)

func TestMetricAvailabilityRoundTrip(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	s := testSession("availability", day(5), "", "short", 64)
	m, _ := testModels()
	rec := NewRecorder(db)
	record(t, rec, []domain.Session{s}, []domain.Model{m}, nil)
	cases := []struct {
		name string
		mask *domain.MetricAvailability
	}{
		{"legacy known zeros", nil},
		{"all absent", &domain.MetricAvailability{}},
		{"prompt tokens zero", &domain.MetricAvailability{PromptTokens: true}},
		{"cached tokens zero", &domain.MetricAvailability{CachedTokens: true}},
		{"prompt duration zero", &domain.MetricAvailability{PromptDuration: true}},
		{"eval tokens zero", &domain.MetricAvailability{EvalTokens: true}},
		{"eval duration zero", &domain.MetricAvailability{EvalDuration: true}},
		{"load duration zero", &domain.MetricAvailability{LoadDuration: true}},
		{"total duration zero", &domain.MetricAvailability{TotalDuration: true}},
	}
	metrics := []domain.ServerMetric{
		domain.MetricPromptTokens, domain.MetricCachedTokens, domain.MetricPromptDuration,
		domain.MetricEvalTokens, domain.MetricEvalDuration, domain.MetricLoadDuration, domain.MetricTotalDuration,
	}
	for i, tc := range cases {
		meas := domain.Measurement{
			Server: domain.ServerMetrics{Available: tc.mask},
			Client: domain.ClientMetrics{Wall: time.Second}, DoneReason: "stop",
		}
		x := exchange(s, m, domain.KindRun, domain.CacheMiss, i+1, day(5).Add(time.Duration(i)*time.Second), meas)
		if err := rec.SaveExchange(t.Context(), x); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		for _, metric := range metrics {
			n := queryValue[sql.Null[int64]](t, db, "SELECT "+string(metric)+" FROM exchanges WHERE run_number = ?", i+1)
			if n.Valid != meas.Server.Has(metric) || n.V != 0 {
				t.Errorf("%s: stored %s = %+v", tc.name, metric, n)
			}
		}
	}
	rows, err := NewSource(db).Rows(t.Context(), report.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(cases) {
		t.Fatalf("%d rows, want %d", len(rows), len(cases))
	}
	for i, r := range rows {
		if r.Measurement.Server.Available == nil {
			t.Fatalf("%s: SQL read did not supply an explicit mask", cases[i].name)
		}
		want := domain.ServerMetrics{Available: cases[i].mask}
		for _, metric := range metrics {
			if got := r.Measurement.Server.Has(metric); got != want.Has(metric) {
				t.Errorf("%s: Has(%s) = %v, want %v", cases[i].name, metric, got, want.Has(metric))
			}
		}
	}
}

func TestTimingOnlyAndFailedExchanges(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	s := testSession("external", day(5), "", "short", 64)
	s.Backend, s.MeasurementProtocol = "openai", "openai-client-v1"
	s.ServerVersion, s.Workload.NumCtx = "", 0
	m := domain.Model{Name: "opaque-model", Identity: "openai:session:external:opaque-model", IdentitySource: "session"}
	rec := NewRecorder(db)
	meas := measurement(1)
	// Nonzero values hidden by the mask must not leak into storage.
	meas.Server.Available = &domain.MetricAvailability{}
	meas.DoneReason = "stop"
	timing := exchange(s, m, domain.KindRun, domain.CacheMiss, 1, day(5), meas)
	noContent := exchange(s, m, domain.KindRun, domain.CacheMiss, 2, day(5).Add(time.Second), meas)
	noContent.Measurement.Client.TimeToFirstToken = 0
	bad := exchange(s, m, domain.KindRun, domain.CacheMiss, 3, day(5).Add(2*time.Second), measurement(2))
	bad.Err = "protocol error" // even nonzero failed measurements must be discarded
	record(t, rec, []domain.Session{s}, []domain.Model{m}, []domain.Exchange{timing, noContent, bad})
	const serverNull = `COALESCE(prompt_tokens, cached_tokens, prompt_ns, eval_tokens, eval_ns, load_ns, total_ns) IS NULL`
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM exchanges WHERE "+serverNull); n != 3 {
		t.Errorf("%d rows with all server metrics NULL, want 3", n)
	}
	if n := queryValue[int](t, db, `SELECT COUNT(*) FROM exchanges WHERE run_number = 1
		AND ttft_ns IS NOT NULL AND wall_ns IS NOT NULL AND done_reason = 'stop' AND error IS NULL`); n != 1 {
		t.Error("successful timing-only exchange lost its client measurements")
	}
	if n := queryValue[sql.Null[int64]](t, db, "SELECT ttft_ns FROM exchanges WHERE run_number = 2"); n.Valid {
		t.Errorf("no-content TTFT = %+v, want NULL", n)
	}
	if n := queryValue[int](t, db, `SELECT COUNT(*) FROM exchanges WHERE run_number = 3
		AND COALESCE(ttft_ns, wall_ns, done_reason) IS NULL AND error = 'protocol error'`); n != 1 {
		t.Error("failed exchange has client or completion measurements")
	}
	rows, err := NewSource(db).Rows(t.Context(), report.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	for i, r := range rows {
		wantMeas := []domain.Measurement{timing.Measurement, noContent.Measurement, {}}[i]
		wantMeas.Server = domain.ServerMetrics{Available: &domain.MetricAvailability{}}
		if !reflect.DeepEqual(r.Measurement, wantMeas) {
			t.Errorf("row %d measurement = %+v, want %+v", i, r.Measurement, wantMeas)
		}
		if r.ServerVersion != "" || r.Workload.NumCtx != 0 || r.ContextPolicy != "server-managed" || r.ServerKind != "unknown" {
			t.Errorf("unknown external facts were invented: %+v", r)
		}
	}
	if err := rec.FinishSession(t.Context(), s.ID, domain.StatusCompleted, day(6)); err != nil {
		t.Fatal(err)
	}
	assertRuns(t, NewSource(db), 3)
}

func TestModelIdentityAndSessionMetadata(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	rec := NewRecorder(db)
	s := testSession("identity", day(5), "deployment", "short", 64)
	s.Backend, s.ServerKind, s.ServerVersion, s.VersionSource = "openai", "vllm", "0.31.0", "operator"
	s.Endpoint, s.MeasurementProtocol, s.ContextPolicy = "https://example.test/proxy/v1", "openai-client-v1", "server-managed"
	s.ConfigJSON = `{"usage":"off","reasoning":"unknown","token_limit_field":"max_tokens"}`
	s.Workload.NumCtx = 0
	native, _ := testModels()
	models := []domain.Model{
		native,
		{Name: "alias", Identity: "openai:model:alias:revision:a", IdentitySource: "operator-revision", Revision: "a"},
		{Name: "alias", Identity: "openai:model:alias:revision:b", IdentitySource: "operator-revision", Revision: "b"},
		{Name: "alias", Identity: "openai:session:identity:alias", IdentitySource: "session"},
		{Name: "verified", Identity: "openai:model:verified:revision:c", IdentitySource: "operator-revision", Revision: "c", Digest: native.Digest},
	}
	for i := 1; i < len(models); i++ {
		models[i].MetadataAvailable = &domain.ModelMetadataAvailability{}
	}
	var xs []domain.Exchange
	for i, m := range models {
		xs = append(xs, exchange(s, m, domain.KindRun, domain.CacheMiss, i+1, day(5).Add(time.Duration(i)*time.Second), measurement(1)))
	}
	record(t, rec, []domain.Session{s}, models, xs)
	alias := models[1]
	alias.Name, alias.Family = "another-alias", "must not replace first row"
	if err := rec.SaveModel(t.Context(), alias); err != nil {
		t.Fatal(err)
	}
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM models"); n != len(models) {
		t.Errorf("%d model rows, want %d", n, len(models))
	}
	const unknown = `COALESCE(digest, format, family, parameter_size, quantization, size_bytes) IS NULL`
	if n := queryValue[int](t, db, "SELECT COUNT(*) FROM models WHERE "+unknown); n != 3 {
		t.Errorf("%d entirely unknown model rows, want 3", n)
	}
	rows, err := NewSource(db).Rows(t.Context(), report.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(xs) {
		t.Fatalf("%d rows, want %d", len(rows), len(xs))
	}
	storedModels := append([]domain.Model(nil), models...)
	storedModels[0].Identity, storedModels[0].IdentitySource = "sha256:aaaa0000aaaa0000", "digest"
	storedModels[0].MetadataAvailable = &domain.ModelMetadataAvailability{SizeBytes: true}
	for i, r := range rows {
		want := wantRow(s, storedModels[i], xs[i])
		want.Measurement.Server.Available = &domain.MetricAvailability{
			PromptTokens: true, CachedTokens: true, PromptDuration: true, EvalTokens: true,
			EvalDuration: true, LoadDuration: true, TotalDuration: true,
		}
		if !reflect.DeepEqual(r, want) {
			t.Errorf("row %d = %+v, want %+v", i, r, want)
		}
		if r.Model.StorageKey() != models[i].StorageKey() {
			t.Errorf("row %d lost its full key: %q", i, r.Model.StorageKey())
		}
	}
	if err := rec.SaveModel(t.Context(), domain.Model{Name: "unidentified"}); err == nil {
		t.Error("SaveModel accepted an alias without a storage identity")
	}
}

func TestBackendEndpointAndCanonicalModelFilters(t *testing.T) {
	t.Parallel()
	db := openTest(t)
	legacy := testSession("legacy", day(1), "", "short", 64)
	native := testSession("native", day(2), "", "short", 64)
	native.Endpoint = "http://ollama.test:11434"
	external := testSession("external", day(3), "", "short", 64)
	external.Backend, external.Endpoint, external.MeasurementProtocol = "openai", "https://a.test/api/v1", "openai-client-v1"
	other := external
	other.ID, other.StartedAt, other.Endpoint = "other", day(4), "https://b.test/api/v1"
	noncanonical := external
	noncanonical.ID, noncanonical.StartedAt, noncanonical.Backend = "noncanonical", day(5), "Ollama"
	noncanonical.Endpoint = "https://c.test/api/v1"
	m, _ := testModels()
	m.Name = "bench:latest"
	opaque := domain.Model{Name: "bench", Identity: "openai:session:external:bench", IdentitySource: "session"}
	tagged := domain.Model{Name: "bench:latest", Identity: "openai:session:external:bench:latest", IdentitySource: "session"}
	portName := domain.Model{Name: "localhost:5000/team/model:latest", Digest: "sha256:port"}
	opaquePath := domain.Model{Name: "Team/Model", Identity: "openai:session:external:Team/Model", IdentitySource: "session"}
	taggedPath := domain.Model{Name: "Team/Model:latest", Identity: "openai:session:external:Team/Model:latest", IdentitySource: "session"}
	xs := []domain.Exchange{
		exchange(legacy, m, domain.KindRun, domain.CacheMiss, 1, day(1), measurement(1)),
		exchange(native, m, domain.KindRun, domain.CacheMiss, 1, day(2), measurement(1)),
		exchange(external, opaque, domain.KindRun, domain.CacheMiss, 1, day(3), measurement(1)),
		exchange(external, tagged, domain.KindRun, domain.CacheMiss, 2, day(3).Add(time.Second), measurement(1)),
		exchange(other, opaque, domain.KindRun, domain.CacheMiss, 1, day(4), measurement(1)),
		exchange(legacy, portName, domain.KindRun, domain.CacheMiss, 2, day(4).Add(time.Second), measurement(1)),
		exchange(noncanonical, tagged, domain.KindRun, domain.CacheMiss, 1, day(5), measurement(1)),
		exchange(external, opaquePath, domain.KindRun, domain.CacheMiss, 3, day(5).Add(time.Second), measurement(1)),
		exchange(external, taggedPath, domain.KindRun, domain.CacheMiss, 4, day(5).Add(2*time.Second), measurement(1)),
		exchange(native, m, domain.KindWarmup, domain.CacheMiss, 2, day(5).Add(3*time.Second), measurement(1)),
		exchange(native, m, domain.KindPrimer, domain.CacheContinue, 2, day(5).Add(4*time.Second), measurement(1)),
	}
	record(t, NewRecorder(db), []domain.Session{legacy, native, external, other, noncanonical}, []domain.Model{m, opaque, tagged, portName, opaquePath, taggedPath}, xs)
	cases := []struct {
		name string
		f    report.Filter
		want []int
	}{
		{"literal plus native shorthand", report.Filter{Models: []string{"bench"}}, []int{0, 1, 2, 4}},
		{"explicit tagged literal", report.Filter{Models: []string{"bench:latest"}}, []int{0, 1, 3, 6}},
		{"overlapping and repeated names do not duplicate rows", report.Filter{Models: []string{"bench", "bench:latest", "bench"}}, []int{0, 1, 2, 3, 4, 6}},
		{"only canonical native backend gets shorthand", report.Filter{Models: []string{"bench"}, Backends: []string{"Ollama"}}, nil},
		{"noncanonical backend retains exact names", report.Filter{Models: []string{"bench:latest"}, Backends: []string{"Ollama"}}, []int{6}},
		{"external path is opaque", report.Filter{Models: []string{"Team/Model"}}, []int{7}},
		{"external explicit path tag is literal", report.Filter{Models: []string{"Team/Model:latest"}}, []int{8}},
		{"external path is case sensitive", report.Filter{Models: []string{"team/model"}}, nil},
		{"external literal only", report.Filter{Models: []string{"bench"}, Backends: []string{"openai"}}, []int{2, 4}},
		{"native backend", report.Filter{Backends: []string{"ollama"}}, []int{0, 1, 5}},
		{"endpoint", report.Filter{Endpoints: []string{external.Endpoint}}, []int{2, 3, 7, 8}},
		{"unknown endpoint explicitly included", report.Filter{Endpoints: []string{"", native.Endpoint}}, []int{0, 1, 5}},
		{"external scoped history", report.Filter{Models: []string{"bench"}, Backends: []string{"openai"}, Endpoints: []string{external.Endpoint}}, []int{2}},
		{"registry port shorthand", report.Filter{Models: []string{"localhost:5000/team/model"}}, []int{5}},
		{"no SQL interpolation", report.Filter{Models: []string{"bench' OR 1=1 --"}}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := NewSource(db).Rows(t.Context(), tc.f)
			if err != nil {
				t.Fatal(err)
			}
			var want []time.Time
			for _, i := range tc.want {
				want = append(want, xs[i].StartedAt.UTC())
			}
			if got := starts(rows); !reflect.DeepEqual(got, want) {
				t.Errorf("starts = %v, want %v", got, want)
			}
		})
	}
}
