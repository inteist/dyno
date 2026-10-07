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
		{Header: "NAME", Cell: func(m domain.Model) string { return format.Text(m.Name) }},
		{Header: "DIGEST", Cell: func(m domain.Model) string {
			if m.MetadataAvailable != nil && m.Digest == "" {
				return format.Missing
			}
			return format.Text(m.ShortDigest())
		}},
		{Header: "FORMAT", Cell: func(m domain.Model) string { return format.Text(m.Format) }},
		{Header: "FAMILY", Cell: func(m domain.Model) string { return format.Text(m.Family) }},
		{Header: "PARAMS", Right: true, Cell: func(m domain.Model) string { return format.Text(m.Parameters) }},
		{Header: "QUANT", Cell: func(m domain.Model) string { return format.Text(m.Quantization) }},
		{Header: "SIZE", Right: true, Cell: func(m domain.Model) string {
			if !m.HasSizeBytes() {
				return format.Missing
			}
			return format.Bytes(m.SizeBytes)
		}},
		{Header: "THINKING", Cell: func(m domain.Model) string {
			if !m.HasThinking() {
				return format.Missing
			}
			return yesNo(m.Thinking)
		}},
	}
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
