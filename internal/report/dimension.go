package report

import (
	"strings"

	"dyno/internal/domain"
)

// Dimension is a field that the report can group by.
type Dimension string

const (
	ByModel       Dimension = "model"
	ByDigest      Dimension = "digest"   // historical name for the full storage identity
	ByModelID     Dimension = "model-id" // same identity as digest
	ByBackend     Dimension = "backend"
	ByEngine      Dimension = "engine"
	ByEndpoint    Dimension = "endpoint"
	ByMeasurement Dimension = "measurement"
	ByServer      Dimension = "server"
	ByWorkload    Dimension = "workload"
	ByMode        Dimension = "mode"
	ByTag         Dimension = "tag"
	ByDay         Dimension = "day"
	ByWeek        Dimension = "week"
	BySession     Dimension = "session"
)

// Dimensions returns every dimension.
func Dimensions() []Dimension {
	return []Dimension{ByModel, ByDigest, ByModelID, ByBackend, ByEngine, ByEndpoint, ByServer, ByMeasurement, ByWorkload, ByMode, ByTag, ByDay, ByWeek, BySession}
}

// DefaultDimensions keep apart all runs that are not comparable.
func DefaultDimensions() []Dimension {
	return []Dimension{ByModel, ByModelID, ByBackend, ByEngine, ByEndpoint, ByServer, ByMeasurement, ByWorkload, ByMode}
}

// ParseDimension converts user input into a Dimension.
func ParseDimension(s string) (Dimension, error) {
	return domain.ParseEnum("dimension", s, Dimensions())
}

// Header returns the column title of d, for example "MODEL".
func (d Dimension) Header() string { return strings.ToUpper(string(d)) }
