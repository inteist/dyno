package domain

import "slices"

// CacheMode selects how the tool prepares the prompt cache before a run.
type CacheMode string

const (
	CacheMiss     CacheMode = "miss"     // new chat, unique ID at the start
	CacheContinue CacheMode = "continue" // next turn of the same chat
	CachePrefix   CacheMode = "prefix"   // new question after the same system prompt
)

// CacheModes returns every cache mode in the canonical order.
func CacheModes() []CacheMode {
	return []CacheMode{CacheMiss, CacheContinue, CachePrefix}
}

// ParseCacheMode converts user input into a CacheMode.
func ParseCacheMode(s string) (CacheMode, error) {
	return ParseEnum("cache mode", s, CacheModes())
}

// Valid reports whether m is a known cache mode.
func (m CacheMode) Valid() bool { return slices.Contains(CacheModes(), m) }
