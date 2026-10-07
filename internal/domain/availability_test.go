package domain

import "testing"

func TestServerAvailabilityDistinguishesMissingFromZero(t *testing.T) {
	t.Parallel()
	known := ServerMetrics{}
	for _, metric := range []ServerMetric{MetricPromptTokens, MetricCachedTokens, MetricPromptDuration, MetricEvalTokens, MetricEvalDuration, MetricLoadDuration, MetricTotalDuration} {
		if !known.Has(metric) {
			t.Errorf("legacy field %s became unknown", metric)
		}
	}
	unknown := ServerMetrics{Available: &MetricAvailability{}}
	if unknown.Has(MetricCachedTokens) || unknown.Has(MetricEvalTokens) || unknown.Has(MetricEvalDuration) {
		t.Fatal("absent metrics became known zero")
	}
	zero := ServerMetrics{Available: &MetricAvailability{CachedTokens: true}}
	if !zero.Has(MetricCachedTokens) || zero.CachedTokens != 0 {
		t.Fatal("present zero was lost")
	}
	if zero.Has("not-a-metric") || known.Has("not-a-metric") {
		t.Fatal("unknown metric accepted")
	}
}

func TestModelStorageKeyDoesNotFabricateDigest(t *testing.T) {
	t.Parallel()
	native := Model{Digest: "abcdef0123456789"}
	if native.StorageKey() != native.Digest {
		t.Fatal("native digest identity changed")
	}
	external := Model{Identity: "openai:revision:abcdef0123456789"}
	if external.StorageKey() != external.Identity || external.Digest != "" || external.ShortDigest() != "abcdef012345" {
		t.Fatalf("bad external key/display: %+v %s", external, external.ShortDigest())
	}
}
