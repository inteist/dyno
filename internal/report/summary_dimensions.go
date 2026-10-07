package report

// AllTimeDimensions is the shared application/renderer history-summary contract.
func AllTimeDimensions() []Dimension {
	return []Dimension{ByModel, ByModelID, ByBackend, ByEngine, ByEndpoint, ByServer, ByMeasurement, ByMode}
}
