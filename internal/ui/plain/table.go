package plain

import (
	"io"
	"strings"
	"unicode/utf8"

	"token-rate/internal/ui/format"
)

// columnGap is the space between two columns.
const columnGap = "  "

// writeTable writes a header row and one row for each item. It is the only
// table writer of the package: reports, session summaries, and models use
// it. The cells of a Right column align to the right.
func writeTable[T any](w io.Writer, cols []format.Column[T], items []T) error {
	lines := format.TableOf(cols, items).Lines(columnGap, utf8.RuneCountInString)
	_, err := io.WriteString(w, strings.Join(lines, "\n")+"\n")
	return err
}
