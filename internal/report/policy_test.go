package report

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"dyno/internal/domain"
)

func TestRowPolicyKeyLegacy(t *testing.T) {
	t.Parallel()
	for _, r := range []Row{
		{},
		{ContextPolicy: domain.LegacyNativeContextPolicy},
		{Backend: domain.BackendOllama, MeasurementProtocol: domain.ProtocolOllama},
		{Backend: domain.BackendOllama, MeasurementProtocol: domain.ProtocolOllama, ContextPolicy: domain.LegacyNativeContextPolicy},
		{Backend: domain.BackendOllama, MeasurementProtocol: domain.ProtocolOllama, ContextPolicy: domain.NativeContextPolicy, ConfigJSON: domain.NativeConfigJSON},
	} {
		if got := r.PolicyKey(); got != "" {
			t.Errorf("legacy policy for %+v = %q, want empty", r, got)
		}
	}
	for _, r := range []Row{
		{ConfigJSON: "{}"},
		{ContextPolicy: "server-managed"},
		{Backend: domain.BackendOpenAI},
		{Backend: domain.BackendOpenAI, ContextPolicy: domain.LegacyNativeContextPolicy},
		{MeasurementProtocol: domain.ProtocolOpenAIChat},
	} {
		got := r.PolicyKey()
		if decoded, err := hex.DecodeString(got); err != nil || len(decoded) != sha256.Size {
			t.Errorf("policy for %+v = %q, want full SHA256", r, got)
		}
	}
	legacy := Build([]Row{{}, {ContextPolicy: domain.LegacyNativeContextPolicy}}, nil, nil)
	if len(legacy.Groups) != 1 || legacy.Groups[0].Key != (Key{}) || len(legacy.Dimensions) != 0 {
		t.Errorf("zero/legacy rows changed: %+v", legacy)
	}
}

func TestRowPolicyKeyCanonicalJSON(t *testing.T) {
	t.Parallel()
	a := Row{
		ContextPolicy: "server-managed",
		ConfigJSON:    `{"seed":9007199254740993,"temperature":0.123456789012345678901,"options":{"z":1e1000,"a":{}},"array":[{"b":2,"a":1},null]}`,
	}
	b := a
	b.ConfigJSON = ` { "array": [ { "a": 1, "b": 2 }, null ], "options": {"a": {}, "z": 1e1000}, "temperature": 0.123456789012345678901, "seed": 9007199254740993 } `
	if got, want := b.PolicyKey(), a.PolicyKey(); got != want {
		t.Errorf("JSON object ordering changed policy: %q != %q", got, want)
	}
	if decoded, err := hex.DecodeString(a.PolicyKey()); err != nil || len(decoded) != sha256.Size {
		t.Errorf("policy = %q, want full SHA256", a.PolicyKey())
	}
}

func TestRowPolicyKeySeparatesPolicies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name               string
		configA, configB   string
		contextA, contextB string
	}{
		{name: "seed omission", configA: `{"omit_seed":false}`, configB: `{"omit_seed":true}`},
		{name: "temperature omission", configA: `{"omit_temperature":false}`, configB: `{"omit_temperature":true}`},
		{name: "unknown versus known empty", configA: "", configB: "{}"},
		{name: "unknown versus known null", configA: "", configB: "null"},
		{name: "integer precision", configA: `{"seed":9007199254740992}`, configB: `{"seed":9007199254740993}`},
		{name: "decimal precision", configA: `{"temperature":0.123456789012345678901}`, configB: `{"temperature":0.123456789012345678902}`},
		{name: "unknown context policies", contextA: "request", contextB: "server-managed"},
		{name: "known empty versus request context", configA: "{}", configB: "{}", contextB: "request"},
		{name: "full context policy", configA: "{}", configB: "{}", contextA: strings.Repeat("context-", 20) + "a", contextB: strings.Repeat("context-", 20) + "b"},
		{name: "malformed config", configA: "{", configB: "["},
		{name: "trailing JSON", configA: "{} {}", configB: "{}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := Row{Backend: domain.BackendOpenAI, MeasurementProtocol: domain.ProtocolOpenAIChat, ConfigJSON: tt.configA, ContextPolicy: tt.contextA}
			b := a
			b.ConfigJSON, b.ContextPolicy = tt.configB, tt.contextB
			if a.PolicyKey() == b.PolicyKey() {
				t.Errorf("distinct policies merged: %+v / %+v", a, b)
			}
			if a.PolicyKey() == "" || b.PolicyKey() == "" {
				t.Error("non-native unknown policies must not use the legacy empty key")
			}
		})
	}
}

func TestBuildSeparatesPolicies(t *testing.T) {
	t.Parallel()
	native := newRow(at(5, 8, 0), 10)
	native.ContextPolicy = domain.LegacyNativeContextPolicy
	configs := []struct {
		name             string
		configA, configB string
		contextB         string
	}{
		{name: "seed omission", configA: `{"omit_seed":false,"omit_temperature":false}`, configB: `{"omit_seed":true,"omit_temperature":false}`},
		{name: "temperature omission", configA: `{"omit_seed":false,"omit_temperature":false}`, configB: `{"omit_seed":false,"omit_temperature":true}`},
		{name: "context policy", configA: "{}", configB: "{}", contextB: "custom-context-policy"},
		{name: "unknown versus known empty", configA: "", configB: "{}"},
		{name: "exact seed", configA: `{"seed":9007199254740992}`, configB: `{"seed":9007199254740993}`},
	}
	dimensions := []struct {
		name string
		dims []Dimension
	}{
		{name: "default", dims: DefaultDimensions()},
		{name: "model", dims: []Dimension{ByModel}},
		{name: "mode", dims: []Dimension{ByMode}},
		{name: "model and mode", dims: []Dimension{ByModel, ByMode}},
		{name: "all-time equivalent", dims: []Dimension{ByBackend, ByEngine, ByEndpoint, ByServer, ByModel, ByModelID, ByWorkload, ByMode}},
		{name: "no dimensions"},
		{name: "empty dimensions", dims: []Dimension{}},
	}
	for name, base := range map[string]Row{"native": native, "compatible": compatibleRow(at(5, 8, 0), true)} {
		for _, cfg := range configs {
			for _, grouping := range dimensions {
				t.Run(name+"/"+cfg.name+"/"+grouping.name, func(t *testing.T) {
					t.Parallel()
					a := base
					a.ConfigJSON = cfg.configA
					a.Model = domain.Model{Name: "served", Identity: "revision:trusted", IdentitySource: "revision"}
					b := a
					b.Session, b.StartedAt, b.ConfigJSON = "s2", at(6, 9, 0), cfg.configB
					if cfg.contextB != "" {
						b.ContextPolicy = cfg.contextB
					}
					dims := slices.Clone(grouping.dims)
					got := Build([]Row{b, a}, dims, nil)
					wantDims := slices.Clone(dims)
					if !slices.Contains(wantDims, ByMeasurement) {
						wantDims = append(wantDims, ByMeasurement)
					}
					if len(got.Groups) != 2 || !slices.Equal(got.Dimensions, wantDims) {
						t.Fatalf("policy grouping = %+v, want two groups and dimensions %v", got, wantDims)
					}
					if !slices.Equal(dims, grouping.dims) {
						t.Error("Build mutated caller dimensions")
					}
					wantPolicies := map[string]bool{a.PolicyKey(): true, b.PolicyKey(): true}
					for _, g := range got.Groups {
						if g.Key.Measurement != a.Protocol() || !wantPolicies[g.Key.Policy] || g.Valid != 1 || g.Sessions != 1 {
							t.Errorf("policy group = %+v", g)
						}
						delete(wantPolicies, g.Key.Policy)
						if slices.Contains(dims, ByWorkload) && g.Key.Workload != a.Workload.Hash() {
							t.Error("policy grouping changed workload identity")
						}
					}
					if len(wantPolicies) != 0 {
						t.Errorf("missing policies: %v", wantPolicies)
					}
					if reversed := keysOf(Build([]Row{a, b}, dims, nil).Groups); !slices.Equal(keysOf(got.Groups), reversed) {
						t.Error("policy group order depends on input order")
					}
				})
			}
		}
	}
}

func TestBuildEquivalentPoliciesMerge(t *testing.T) {
	t.Parallel()
	for name, base := range map[string]Row{"native": newRow(at(5, 8, 0), 10), "compatible": compatibleRow(at(5, 8, 0), true)} {
		for _, dims := range [][]Dimension{DefaultDimensions(), {ByModel, ByMode}, nil} {
			t.Run(name+"/"+strings.Join(dimensionNames(dims), ","), func(t *testing.T) {
				t.Parallel()
				a := base
				b := a
				a.ConfigJSON = `{"omit_seed":false,"options":{"seed":9007199254740993,"temperature":1e1000}}`
				b.ConfigJSON = ` { "options": {"temperature": 1e1000, "seed": 9007199254740993}, "omit_seed": false } `
				b.Session = "s2"
				got := Build([]Row{a, b}, dims, nil)
				if len(got.Groups) != 1 || got.Groups[0].Valid != 2 || got.Groups[0].Sessions != 2 {
					t.Fatalf("equivalent JSON policies split: %+v", got)
				}
				if slices.Contains(got.Dimensions, ByMeasurement) && got.Groups[0].Key.Policy != a.PolicyKey() {
					t.Errorf("canonical policy key = %q, want %q", got.Groups[0].Key.Policy, a.PolicyKey())
				}
				if name == "native" && !slices.Equal(got.Dimensions, dims) {
					t.Error("equal native policies should preserve custom dimensions")
				}
			})
		}
	}
}

func TestMeasurementValueAndOrderingUseFullPolicy(t *testing.T) {
	t.Parallel()
	legacy := Key{Measurement: domain.ProtocolOllama}
	if got := legacy.Value(ByMeasurement); got != legacy.Measurement {
		t.Errorf("legacy measurement = %q, want protocol alone", got)
	}
	a := Key{Measurement: domain.ProtocolOpenAIChat, Policy: strings.Repeat("a", 63) + "1"}
	b := a
	b.Policy = strings.Repeat("a", 63) + "2"
	if got, want := a.Value(ByMeasurement), a.Measurement+" policy-"+a.Policy; got != want {
		t.Errorf("measurement value = %q, want %q", got, want)
	}
	compare := compareKeys([]Dimension{ByMeasurement})
	if compare(a, b) >= 0 || compare(b, a) <= 0 {
		t.Error("policy ordering ignored the end of the full fingerprint")
	}
}

func dimensionNames(dims []Dimension) []string {
	names := make([]string, len(dims))
	for i, d := range dims {
		names[i] = string(d)
	}
	return names
}
