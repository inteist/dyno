package report

import "testing"

func BenchmarkBuildRepeatedPolicy(b *testing.B) {
	rows := make([]Row, 2000)
	base := compatibleRow(at(5, 8, 0), true)
	base.ConfigJSON = `{"reasoning_policy":"disabled","omit_seed":false,"omit_temperature":false,"options":{"seed":42,"temperature":0}}`
	for i := range rows {
		rows[i] = base
	}
	b.ReportAllocs()
	for b.Loop() {
		Build(rows, DefaultDimensions(), nil)
	}
}
