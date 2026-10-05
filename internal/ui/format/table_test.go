package format

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

func sampleTable() Table {
	type row struct{ name, n, note string }
	cols := []Column[row]{
		{Header: "NAME", Cell: func(r row) string { return r.name }},
		{Header: "N", Right: true, Cell: func(r row) string { return r.n }},
		{Header: "NOTE", Cell: func(r row) string { return r.note }},
	}
	return TableOf(cols, []row{{"qwen", "60", "2 short"}, {"lfm2.5:8b", "3", ""}})
}

func TestTableLines(t *testing.T) {
	t.Parallel()
	got := sampleTable().Lines("  ", utf8.RuneCountInString)
	want := []string{
		"NAME        N  NOTE",
		"qwen       60  2 short",
		"lfm2.5:8b   3",
	}
	if !slices.Equal(got, want) {
		t.Errorf("Lines() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if w := sampleTable().Width("  ", utf8.RuneCountInString); w != 9+2+2+2+7 {
		t.Errorf("Width() = %d", w)
	}
}

func TestTableWithOnlyMap(t *testing.T) {
	t.Parallel()
	base := sampleTable()
	wide := base.With("LAST", []string{"a", "b"})
	if len(base.Header) != 3 || !slices.Equal(wide.Header, []string{"NAME", "N", "NOTE", "LAST"}) || wide.Rows[1][3] != "b" {
		t.Fatalf("With() = %+v, base = %+v", wide, base)
	}
	narrow := wide.Only([]bool{true, true, false, true})
	if !slices.Equal(narrow.Header, []string{"NAME", "N", "LAST"}) || !slices.Equal(narrow.Right, []bool{false, true, false}) {
		t.Errorf("Only() = %+v", narrow)
	}
	upper := base.Map(strings.ToLower, strings.ToUpper)
	if upper.Header[0] != "name" || upper.Rows[0][0] != "QWEN" || base.Rows[0][0] != "qwen" {
		t.Errorf("Map() = %+v, base = %+v", upper, base)
	}
	if kept := base.Map(nil, nil); !slices.Equal(kept.Rows[0], base.Rows[0]) {
		t.Errorf("Map(nil, nil) = %+v", kept)
	}
	// A table without alignments aligns every column to the left.
	plain := Table{Header: []string{"A", "B"}, Rows: [][]string{{"1", "22"}}}
	if got := plain.Lines(" ", utf8.RuneCountInString); !slices.Equal(got, []string{"A B", "1 22"}) {
		t.Errorf("Lines() without Right = %q", got)
	}
}

func TestJustify(t *testing.T) {
	t.Parallel()
	if got := Justify("left", "right", 15, 2, utf8.RuneCountInString); got != "left      right" {
		t.Errorf("Justify() = %q", got)
	}
	if got := Justify("left", "right", 5, 2, utf8.RuneCountInString); got != "left  right" {
		t.Errorf("Justify() in a narrow line = %q", got)
	}
}
