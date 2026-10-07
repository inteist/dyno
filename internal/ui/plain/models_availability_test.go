package plain

import (
	"testing"

	"dyno/internal/domain"
	"dyno/internal/ui/format"
)

func TestModelMetadataUnknownIsNotZeroOrNo(t *testing.T) {
	t.Parallel()
	unknown := domain.Model{Name: "served-id", Identity: "openai:revision:123456789abcdef", MetadataAvailable: &domain.ModelMetadataAvailability{}}
	known := domain.Model{MetadataAvailable: &domain.ModelMetadataAvailability{SizeBytes: true, Thinking: true}}
	for _, column := range modelColumns() {
		switch column.Header {
		case "DIGEST", "FORMAT", "FAMILY", "PARAMS", "QUANT", "SIZE", "THINKING":
			if got := column.Cell(unknown); got != format.Missing {
				t.Errorf("unknown %s=%q", column.Header, got)
			}
		}
		if column.Header == "SIZE" && column.Cell(known) != "0 B" {
			t.Fatal("known zero size lost")
		}
		if column.Header == "THINKING" && column.Cell(known) != "no" {
			t.Fatal("known unsupported thinking lost")
		}
	}
}
