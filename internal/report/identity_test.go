package report

import (
	"slices"
	"testing"

	"dyno/internal/domain"
)

func TestEndpointDisplayFactsSurviveWithoutEndpointDimension(t *testing.T) {
	a, b := newRow(at(5, 8, 0), 10), newRow(at(5, 9, 0), 20)
	a.Endpoint, b.Endpoint = "https://first/v1", "https://second/v1"
	r := Build([]Row{a, b}, []Dimension{ByModel}, nil)
	if len(r.Groups) != 1 || !slices.Equal(r.Groups[0].Endpoints, []string{a.Endpoint, b.Endpoint}) || r.Groups[0].Key.Endpoint != "" {
		t.Fatal("display endpoints changed grouping or were lost", r.Groups)
	}
}

func TestDefaultDimensionOrder(t *testing.T) {
	t.Parallel()
	want := []Dimension{ByModel, ByModelID, ByBackend, ByEngine, ByEndpoint, ByServer, ByMeasurement, ByWorkload, ByMode}
	if got := DefaultDimensions(); !slices.Equal(got, want) {
		t.Errorf("default dimensions = %v, want %v", got, want)
	}
}

func TestHumanIdentityFactsDoNotSplitStorageKeys(t *testing.T) {
	t.Parallel()
	for _, d := range []Dimension{ByDigest, ByModelID} {
		a, b := newRow(at(5, 8, 0), 10), newRow(at(5, 9, 0), 20)
		a.Model = domain.Model{Name: "same", Identity: "openai:revision:trusted"}
		b.Model = a.Model
		b.Model.IdentitySource, b.Model.Revision = "operator-revision", "release-a"
		got := Build([]Row{a, b}, []Dimension{d}, nil)
		if len(got.Groups) != 1 || got.Groups[0].Valid != 2 {
			t.Fatalf("display-only identity facts split the full key: %+v", got)
		}
		if key := got.Groups[0].Key; key.Digest != a.Model.StorageKey() || key.IdentitySource != b.Model.IdentitySource || key.Revision != b.Model.Revision {
			t.Errorf("group failed to retain known display facts: %+v", key)
		}
	}
}

func TestIdentityDisplayFactsTravelWithFullGroupingKeys(t *testing.T) {
	t.Parallel()
	for _, d := range []Dimension{ByDigest, ByModelID} {
		a, b := newRow(at(5, 8, 0), 10), newRow(at(5, 9, 0), 20)
		a.Model = domain.Model{Name: "same", Identity: "openai:revision:0123456789ab1111", IdentitySource: "operator-revision", Revision: "release-a"}
		b.Model = a.Model
		b.Model.Identity = "openai:revision:0123456789ab2222"
		b.Model.Revision = "release-b"
		r := Build([]Row{a, b}, []Dimension{d}, nil)
		if len(r.Groups) != 2 {
			t.Fatalf("%s grouped by shortened display: %+v", d, r.Groups)
		}
		for i, model := range []domain.Model{a.Model, b.Model} {
			key := r.Groups[i].Key
			if key.Digest != model.StorageKey() || key.Value(d) != model.StorageKey() || key.IdentitySource != model.IdentitySource || key.Revision != model.Revision {
				t.Errorf("%s lost full identity/display facts: %+v / %+v", d, key, model)
			}
		}
	}
}
