package cli

import (
	"errors"
	"flag"
	"strings"
	"time"

	"dyno/internal/domain"
)

// value is a flag.Value that converts its text with parse and shows its
// current value with format.
type value[T any] struct {
	target *T
	parse  func(string) (T, error)
	format func(T) string
}

// String returns the current value. The flag package can call it on a zero
// value.
func (v *value[T]) String() string {
	if v == nil || v.target == nil {
		return ""
	}
	return v.format(*v.target)
}

// Set converts s and stores the result.
func (v *value[T]) Set(s string) error {
	parsed, err := v.parse(s)
	if err != nil {
		return err
	}
	*v.target = parsed
	return nil
}

// listValue returns a flag.Value for a comma-separated list. parse converts
// each item. A new list replaces the old one, so the last flag wins.
func listValue[T ~string](target *[]T, parse func(string) (T, error)) flag.Value {
	return &value[[]T]{
		target: target,
		parse:  func(s string) ([]T, error) { return parseList(s, parse) },
		format: domain.JoinEnum[T],
	}
}

// parseList splits s at the commas, trims the spaces, drops the empty items,
// and converts each item with parse.
func parseList[T any](s string, parse func(string) (T, error)) ([]T, error) {
	var items []T
	for text := range strings.SplitSeq(s, ",") {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		item, err := parse(text)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// asIs converts a list item without a check.
func asIs[T ~string](s string) (T, error) { return T(s), nil }

// orderValue returns a flag.Value for a domain.Order.
func orderValue(target *domain.Order) flag.Value {
	return &value[domain.Order]{
		target: target,
		parse:  domain.ParseOrder,
		format: func(o domain.Order) string { return string(o) },
	}
}

// dateValue returns a flag.Value for a time in the form YYYY-MM-DD or
// RFC 3339. A date gives midnight in loc, extraDays later. --until passes 1,
// so that its day counts in full.
func dateValue(target *time.Time, loc *time.Location, extraDays int) flag.Value {
	return &value[time.Time]{
		target: target,
		parse: func(s string) (time.Time, error) {
			return parseTime(s, loc, extraDays)
		},
		format: formatTime,
	}
}

// parseTime reads s as a date in loc, extraDays later, or as an RFC 3339 time.
func parseTime(s string, loc *time.Location, extraDays int) (time.Time, error) {
	if day, err := time.ParseInLocation(time.DateOnly, s, loc); err == nil {
		return day.AddDate(0, 0, extraDays), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("use YYYY-MM-DD or RFC 3339, for example 2026-10-05 or 2026-10-05T14:00:00+02:00")
}

// formatTime returns t in RFC 3339, or "" for the zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
