package report

import (
	"time"

	"token-rate/internal/domain"
)

// Limits of the validity rules.
const (
	// A miss run may take up to 1 % of the prompt from the cache, or up to
	// maxMissCachedTokens tokens. The tokens of the chat template before the
	// unique ID always match, and they exceed 1 % of a short prompt.
	maxMissCachedShare  = 0.01
	maxMissCachedTokens = 16
	// A run may report a load of up to 1 s. The MLX engine reports about
	// 0.1 s for the first request after a warm-up, but a real load takes
	// seconds.
	maxLoadInRun = time.Second
)

// Classify returns the flags of a row. A row without flags counts in the
// statistics. A failed row gets only FlagFailed, because it has no
// measurement. A low cached share in prefix mode is a result, not an error,
// so Classify does not flag it.
func Classify(r Row) []Flag {
	if r.Err != "" {
		return []Flag{FlagFailed}
	}
	var flags []Flag
	m := r.Measurement
	if m.DoneReason != domain.DoneLength {
		flags = append(flags, FlagShort)
	}
	if r.Mode == domain.CacheMiss && unexpectedHit(m.Server) {
		flags = append(flags, FlagUnexpectedHit)
	}
	if m.Server.LoadDuration > maxLoadInRun {
		flags = append(flags, FlagLoadInRun)
	}
	return flags
}

// unexpectedHit reports whether a miss run took more from the cache than
// the chat template.
func unexpectedHit(m domain.ServerMetrics) bool {
	return m.CachedShare() > maxMissCachedShare && m.CachedTokens > maxMissCachedTokens
}

// partition applies the validity rules to rows. It returns the rows without
// flags in their order, and the count of each flag of the other rows.
func partition(rows []Row) (valid []Row, excluded map[Flag]int) {
	excluded = make(map[Flag]int)
	for _, r := range rows {
		flags := Classify(r)
		if len(flags) == 0 {
			valid = append(valid, r)
		}
		for _, f := range flags {
			excluded[f]++
		}
	}
	return valid, excluded
}
