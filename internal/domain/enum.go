package domain

import (
	"fmt"
	"slices"
	"strings"
)

// ParseEnum returns s as a T if s is one of valid. kind names the type in
// the error message.
func ParseEnum[T ~string](kind, s string, valid []T) (T, error) {
	v := T(s)
	if !slices.Contains(valid, v) {
		return "", fmt.Errorf("unknown %s %q: use one of %s", kind, s, JoinEnum(valid))
	}
	return v, nil
}

// JoinEnum returns the values separated by commas and spaces.
func JoinEnum[T ~string](values []T) string {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = string(v)
	}
	return strings.Join(names, ", ")
}
