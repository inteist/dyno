package sqlite

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"dyno/internal/domain"
)

// timeLayout is the form of every stored time: UTC with a fixed width, so
// that text order is time order.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

// formatTime returns t as stored text.
func formatTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// parseTime reads a time that formatTime wrote.
func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored time: %w", err)
	}
	return t, nil
}

// joinModes returns the modes separated by commas, in their order.
func joinModes(modes []domain.CacheMode) string {
	names := make([]string, len(modes))
	for i, m := range modes {
		names[i] = string(m)
	}
	return strings.Join(names, ",")
}

// timeColumn scans a time that formatTime wrote into dst.
type timeColumn struct{ dst *time.Time }

// Scan implements sql.Scanner.
func (c timeColumn) Scan(src any) error {
	var s sql.Null[string]
	if err := s.Scan(src); err != nil {
		return err
	}
	t, err := parseTime(s.V) // NULL fails here
	if err != nil {
		return err
	}
	*c.dst = t
	return nil
}

// orZero returns a scanner for a column that can be NULL. NULL gives the
// zero value of T.
func orZero[T any](dst *T) sql.Scanner { return nullable[T]{dst: dst} }

// metricColumn scans a raw metric and records whether it was supplied. A
// present zero remains known; NULL clears both the value and its availability.
func metricColumn[T any](dst *T, known *bool) sql.Scanner {
	return nullable[T]{dst: dst, known: known}
}

// optionalText writes unknown strings as NULL instead of inventing a value.
func optionalText(s string) sql.Null[string] { return sql.Null[string]{V: s, Valid: s != ""} }

// nullable scans a column that can be NULL into dst and optionally records
// its presence.
type nullable[T any] struct {
	dst   *T
	known *bool
}

// Scan implements sql.Scanner.
func (c nullable[T]) Scan(src any) error {
	var n sql.Null[T]
	if err := n.Scan(src); err != nil {
		return err
	}
	*c.dst = n.V
	if c.known != nil {
		*c.known = n.Valid
	}
	return nil
}
