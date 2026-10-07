package report

import (
	"slices"
	"testing"
	"time"

	"dyno/internal/domain"
)

// dimensionRows returns three valid runs that differ in every dimension.
func dimensionRows() []Row {
	a, b, c := newRow(at(5, 8, 0), 10), newRow(at(6, 8, 0), 20), newRow(at(12, 8, 0), 30)
	a.Model = domain.Model{Name: "a", Digest: "d1d1d1d1d1d1ffff"}
	b.Model = domain.Model{Name: "b", Digest: "d2d2d2d2d2d2ffff"}
	c.Model = domain.Model{Name: "a", Digest: "d2d2d2d2d2d2ffff"}
	a.ServerVersion, b.ServerVersion, c.ServerVersion = "0.34.0", "0.33.0", "0.34.0"
	a.Workload, b.Workload, c.Workload = workload("h1"), workload("h2"), workload("h2")
	a.Mode, b.Mode, c.Mode = domain.CachePrefix, domain.CacheMiss, domain.CacheContinue
	a.Tag, b.Tag, c.Tag = "t1", "t2", "t1"
	a.Session, b.Session, c.Session = "s1", "s2", "s2"
	return []Row{a, b, c}
}

func TestBuildGroupsByDimension(t *testing.T) {
	t.Parallel()
	// The hash of h1 sorts before the hash of h2.
	h1, h2 := workload("h1").Hash(), workload("h2").Hash()
	days := []Key{{Bucket: "2026-10-05"}, {Bucket: "2026-10-06"}, {Bucket: "2026-10-12"}}
	tests := []struct {
		name  string
		dims  []Dimension
		want  []Key
		sizes []int
	}{
		{name: "model", dims: []Dimension{ByModel}, want: []Key{{Model: "a"}, {Model: "b"}}, sizes: []int{2, 1}},
		{
			name: "digest", dims: []Dimension{ByDigest},
			want: []Key{{Digest: "d1d1d1d1d1d1ffff"}, {Digest: "d2d2d2d2d2d2ffff"}}, sizes: []int{1, 2},
		},
		{name: "server", dims: []Dimension{ByServer}, want: []Key{{Server: "0.33.0"}, {Server: "0.34.0"}}, sizes: []int{1, 2}},
		{name: "workload", dims: []Dimension{ByWorkload}, want: []Key{{Workload: h1}, {Workload: h2}}, sizes: []int{1, 2}},
		{
			name: "mode", dims: []Dimension{ByMode},
			want: []Key{{Mode: "miss"}, {Mode: "continue"}, {Mode: "prefix"}}, sizes: []int{1, 1, 1},
		},
		{name: "tag", dims: []Dimension{ByTag}, want: []Key{{Tag: "t1"}, {Tag: "t2"}}, sizes: []int{2, 1}},
		{name: "session", dims: []Dimension{BySession}, want: []Key{{Session: "s1"}, {Session: "s2"}}, sizes: []int{1, 2}},
		{name: "day", dims: []Dimension{ByDay}, want: days, sizes: []int{1, 1, 1}},
		{name: "week", dims: []Dimension{ByWeek}, want: []Key{{Bucket: "2026-W41"}, {Bucket: "2026-W42"}}, sizes: []int{2, 1}},
		{name: "day wins over week", dims: []Dimension{ByWeek, ByDay}, want: days, sizes: []int{1, 1, 1}},
		{
			name: "default", dims: DefaultDimensions(),
			want: []Key{
				{Backend: domain.BackendOllama, Engine: domain.BackendOllama, Measurement: domain.ProtocolOllama, Model: "a", Digest: "d1d1d1d1d1d1ffff", Server: "0.34.0", Workload: h1, Mode: "prefix"},
				{Backend: domain.BackendOllama, Engine: domain.BackendOllama, Measurement: domain.ProtocolOllama, Model: "a", Digest: "d2d2d2d2d2d2ffff", Server: "0.34.0", Workload: h2, Mode: "continue"},
				{Backend: domain.BackendOllama, Engine: domain.BackendOllama, Measurement: domain.ProtocolOllama, Model: "b", Digest: "d2d2d2d2d2d2ffff", Server: "0.33.0", Workload: h2, Mode: "miss"},
			},
			sizes: []int{1, 1, 1},
		},
		{
			name: "model and mode", dims: []Dimension{ByModel, ByMode},
			want:  []Key{{Model: "a", Mode: "continue"}, {Model: "a", Mode: "prefix"}, {Model: "b", Mode: "miss"}},
			sizes: []int{1, 1, 1},
		},
		{
			name: "mode and model", dims: []Dimension{ByMode, ByModel},
			want:  []Key{{Model: "b", Mode: "miss"}, {Model: "a", Mode: "continue"}, {Model: "a", Mode: "prefix"}},
			sizes: []int{1, 1, 1},
		},
		{name: "no dimensions", dims: nil, want: []Key{{}}, sizes: []int{3}},
		{name: "empty dimensions", dims: []Dimension{}, want: []Key{{}}, sizes: []int{3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := Build(dimensionRows(), tt.dims, nil)
			if got := keysOf(r.Groups); !slices.Equal(got, tt.want) {
				t.Fatalf("keys =\n%+v\nwant\n%+v", got, tt.want)
			}
			for i, g := range r.Groups {
				if g.Decode.N != tt.sizes[i] {
					t.Errorf("group %+v has %d runs, want %d", g.Key, g.Decode.N, tt.sizes[i])
				}
			}
			if !slices.Equal(r.Dimensions, tt.dims) {
				t.Errorf("Dimensions = %v, want %v", r.Dimensions, tt.dims)
			}
		})
	}
}

func TestBuildBuckets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		at   time.Time
		dims []Dimension
		loc  *time.Location
		want string
	}{
		{name: "day before local midnight", at: at(5, 21, 59), dims: []Dimension{ByDay}, loc: cest(), want: "2026-10-05"},
		{name: "late UTC time on the next local day", at: at(5, 22, 30), dims: []Dimension{ByDay}, loc: cest(), want: "2026-10-06"},
		{name: "nil location means UTC", at: at(5, 22, 30), dims: []Dimension{ByDay}, loc: nil, want: "2026-10-05"},
		{name: "week", at: at(5, 8, 0), dims: []Dimension{ByWeek}, loc: cest(), want: "2026-W41"},
		{name: "late UTC Sunday in the next local week", at: at(11, 22, 30), dims: []Dimension{ByWeek}, loc: cest(), want: "2026-W42"},
		{name: "same Sunday in UTC", at: at(11, 22, 30), dims: []Dimension{ByWeek}, loc: nil, want: "2026-W41"},
		{
			name: "last day of 2026", at: time.Date(2026, time.December, 31, 12, 0, 0, 0, time.UTC),
			dims: []Dimension{ByWeek}, loc: cest(), want: "2026-W53",
		},
		{
			name: "first day of 2027", at: time.Date(2027, time.January, 1, 12, 0, 0, 0, time.UTC),
			dims: []Dimension{ByWeek}, loc: cest(), want: "2026-W53",
		},
		{
			name: "late UTC time on the first local day of 2027-W01", at: time.Date(2027, time.January, 3, 22, 30, 0, 0, time.UTC),
			dims: []Dimension{ByWeek}, loc: cest(), want: "2027-W01",
		},
		{
			name: "day wins over week", at: at(5, 22, 30),
			dims: []Dimension{ByWeek, ByDay}, loc: cest(), want: "2026-10-06",
		},
		{name: "no time dimension", at: at(5, 8, 0), dims: []Dimension{ByModel}, loc: cest(), want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := Build([]Row{newRow(tt.at, 10)}, tt.dims, tt.loc)
			if len(r.Groups) != 1 {
				t.Fatalf("got %d groups, want 1", len(r.Groups))
			}
			if got := r.Groups[0].Key.Bucket; got != tt.want {
				t.Errorf("Bucket = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildGroupOrder(t *testing.T) {
	t.Parallel()
	var rows []Row
	for i, name := range []string{"b", "a"} {
		for j, mode := range []domain.CacheMode{"zeta", domain.CachePrefix, "alpha", domain.CacheContinue, domain.CacheMiss} {
			r := newRow(at(5, 8+i, j), 10)
			r.Model.Name, r.Mode = name, mode
			rows = append(rows, r)
		}
	}
	want := []Key{
		{Model: "a", Mode: "miss"}, {Model: "a", Mode: "continue"}, {Model: "a", Mode: "prefix"},
		{Model: "a", Mode: "alpha"}, {Model: "a", Mode: "zeta"},
		{Model: "b", Mode: "miss"}, {Model: "b", Mode: "continue"}, {Model: "b", Mode: "prefix"},
		{Model: "b", Mode: "alpha"}, {Model: "b", Mode: "zeta"},
	}
	for name, input := range map[string][]Row{"given order": rows, "shuffled": shuffled(rows)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := keysOf(Build(input, []Dimension{ByModel, ByMode}, nil).Groups); !slices.Equal(got, want) {
				t.Errorf("keys =\n%+v\nwant\n%+v", got, want)
			}
		})
	}
}
