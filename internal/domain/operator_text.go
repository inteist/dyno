package domain

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsAPIKey rejects invalid encoding and characters unsafe in HTTP credentials.
func IsAPIKey(text string) bool {
	return utf8.ValidString(text) && !strings.ContainsFunc(text, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029'
	})
}

// IsOperatorText accepts bounded printable labels, including empty optional ones.
// Newlines, terminal escapes and invisible formatting controls are not labels.
func IsOperatorText(text string) bool {
	return utf8.RuneCountInString(text) <= 64 && IsPrintableText(text)
}

// IsModelRevision accepts printable unpadded labels or a standard SHA-256 digest.
func IsModelRevision(text string) bool {
	if strings.TrimSpace(text) != text {
		return false
	}
	if strings.HasPrefix(text, "sha256:") {
		if len(text) != 71 {
			return false
		}
		_, err := hex.DecodeString(text[7:])
		return err == nil
	}
	return IsOperatorText(text)
}

// IsPrintableText leaves historical tag length unrestricted while excluding controls.
func IsPrintableText(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}
