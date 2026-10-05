package report

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"token-rate/internal/domain"
)

// dayLayout formats a day bucket, for example "2026-10-05".
const dayLayout = "2006-01-02"

// groupRows splits rows by their key. Each part keeps the order of rows.
func groupRows(rows []Row, dims []Dimension, loc *time.Location) map[Key][]Row {
	parts := make(map[Key][]Row)
	for _, r := range rows {
		k := keyOf(r, dims, loc)
		parts[k] = append(parts[k], r)
	}
	return parts
}

// keyOf returns the key of r. Only the fields of dims are set.
func keyOf(r Row, dims []Dimension, loc *time.Location) Key {
	k := Key{Bucket: bucketOf(r.StartedAt, dims, loc)}
	for _, d := range dims {
		setField(&k, d, r)
	}
	return k
}

// setField copies the value of r that d selects into k. bucketOf serves the
// time dimensions.
func setField(k *Key, d Dimension, r Row) {
	switch d {
	case ByModel:
		k.Model = r.Model.Name
	case ByDigest:
		k.Digest = r.Model.ShortDigest()
	case ByOllama:
		k.Ollama = r.OllamaVersion
	case ByWorkload:
		k.Workload = r.Workload.Hash()
	case ByMode:
		k.Mode = string(r.Mode)
	case ByTag:
		k.Tag = r.Tag
	case BySession:
		k.Session = string(r.Session)
	}
}

// bucketOf returns the local day or the ISO week of t, as dims ask. The day
// wins over the week. Without either, it returns "".
func bucketOf(t time.Time, dims []Dimension, loc *time.Location) string {
	switch {
	case slices.Contains(dims, ByDay):
		return dayOf(t, loc)
	case slices.Contains(dims, ByWeek):
		return weekOf(t, loc)
	}
	return ""
}

// dayOf returns the local day of t, for example "2026-10-05".
func dayOf(t time.Time, loc *time.Location) string {
	return midnight(t, loc).Format(dayLayout)
}

// weekOf returns the local ISO week of t, for example "2026-W41".
func weekOf(t time.Time, loc *time.Location) string {
	year, week := t.In(loc).ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// midnight returns the start of the local day of t.
func midnight(t time.Time, loc *time.Location) time.Time {
	year, month, day := t.In(loc).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, loc)
}

// compareKeys returns the order of the groups: by the values of dims, in the
// order of dims.
func compareKeys(dims []Dimension) func(a, b Key) int {
	return func(a, b Key) int {
		for _, d := range dims {
			if c := compareValues(d, a.Value(d), b.Value(d)); c != 0 {
				return c
			}
		}
		return 0
	}
}

// compareValues orders two values of d. Cache modes take the canonical
// order, and all other values take the text order.
func compareValues(d Dimension, a, b string) int {
	if d == ByMode {
		return cmp.Or(cmp.Compare(modeRank(a), modeRank(b)), strings.Compare(a, b))
	}
	return strings.Compare(a, b)
}

// modeRank returns the place of s in domain.CacheModes. Unknown modes come
// after the known ones.
func modeRank(s string) int {
	modes := domain.CacheModes()
	if i := slices.Index(modes, domain.CacheMode(s)); i >= 0 {
		return i
	}
	return len(modes)
}
