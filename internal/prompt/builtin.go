package prompt

import (
	"fmt"
	"strings"

	"dyno/internal/domain"
)

// Names of the built-in sets.
const (
	Short = "short"
	Long  = "long"
)

// Builtin returns the built-in set with name.
func Builtin(name string) (Set, bool) {
	switch name {
	case Short:
		return short(), true
	case Long:
		return long(), true
	}
	return Set{}, false
}

// BuiltinNames returns the names of the built-in sets.
func BuiltinNames() []string { return []string{Short, Long} }

// DefaultFor returns the name of the smallest built-in set that serves every
// mode of modes.
func DefaultFor(modes []domain.CacheMode) string {
	if short().SupportsAll(modes) {
		return Short
	}
	return Long
}

// short has no system part. Its question asks for more text than any
// num_predict default, so that each model stops at the limit.
func short() Set {
	return Set{
		Name: Short,
		Question: "Write a story of at least 1,500 words about a lighthouse keeper who records " +
			"the weather every day. Tell the story in many short chapters.",
	}
}

// long has a system part of about 4,300 tokens. The code builds the text, so
// it is the same in every build. Both questions ask for more text than the
// num_predict default, so that each model stops at the limit.
func long() Set {
	var b strings.Builder
	for i := range 200 {
		fmt.Fprintf(&b, "Log %d: the keeper recorded wind at %d knots and visibility of %d miles. ",
			i, i*7%31, i*13%17)
	}
	return Set{
		Name:     Long,
		System:   strings.TrimSpace(b.String()),
		Question: "Retell the log entry by entry, with one full paragraph for each entry. Write at least 1,500 words.",
		FollowUp: "Now retell the log again in reverse order, with one full paragraph for each entry. Write at least 1,500 words.",
	}
}
