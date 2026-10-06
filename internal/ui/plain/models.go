package plain

import (
	"fmt"
	"io"

	"dyno/internal/domain"
	"dyno/internal/ui/format"
)

// WriteModels writes the table of the local models.
func WriteModels(w io.Writer, models []domain.Model) error {
	if err := writeTable(w, modelColumns(), models); err != nil {
		return fmt.Errorf("write models: %w", err)
	}
	return nil
}

func modelColumns() []format.Column[domain.Model] {
	return []format.Column[domain.Model]{
		{Header: "NAME", Cell: func(m domain.Model) string { return m.Name }},
		{Header: "DIGEST", Cell: domain.Model.ShortDigest},
		{Header: "FORMAT", Cell: func(m domain.Model) string { return m.Format }},
		{Header: "FAMILY", Cell: func(m domain.Model) string { return m.Family }},
		{Header: "PARAMS", Right: true, Cell: func(m domain.Model) string { return m.Parameters }},
		{Header: "QUANT", Cell: func(m domain.Model) string { return m.Quantization }},
		{Header: "SIZE", Right: true, Cell: func(m domain.Model) string { return format.Bytes(m.SizeBytes) }},
		{Header: "THINKING", Cell: func(m domain.Model) string { return yesNo(m.Thinking) }},
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
