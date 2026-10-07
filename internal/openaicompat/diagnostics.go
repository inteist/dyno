package openaicompat

import (
	"encoding/json"
	"html"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/openai/openai-go/v3"
)

const maxDiagnosticChars = 200

var (
	diagnosticURL      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`)
	bearerCredential   = regexp.MustCompile(`(?i)\bbearer\s+[^\s"',;]+`)
	labelledCredential = regexp.MustCompile(`(?i)\b(api[_ -]?key|authorization|password|access[_ -]?token|cookie|secret)["']?\s*[:=]\s*["']?[^\s"',;]+`)
)

// newSanitizer is used only at the transport boundary. Resolved credentials are
// never copied into provenance, returned diagnostics, or wrapped error causes.
func newSanitizer(key string, endpoint *url.URL) func(string) string {
	secrets := []string{key, url.QueryEscape(key), url.PathEscape(key)}
	if path := strings.TrimSuffix(endpoint.Path, "/"); path != "" {
		secrets = append(secrets, path, strings.TrimSuffix(endpoint.EscapedPath(), "/"))
	}
	// One replacement pass, longest variants first. Protect existing markers:
	// short keys such as "a" must not re-redact the marker itself.
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	patterns := []string{regexp.QuoteMeta("[redacted]")}
	for _, secret := range secrets {
		if secret != "" {
			parts := make([]string, 0, len(secret))
			for _, r := range secret {
				parts = append(parts, regexp.QuoteMeta(string(r)))
			}
			patterns = append(patterns, strings.Join(parts, `[\p{Cc}\p{Cf}\x{2028}\x{2029}]*`))
		}
	}
	redact, err := regexp.Compile("(?i)(?:" + strings.Join(patterns, "|") + ")")
	if err != nil {
		return func(string) string { return "[redacted]" }
	}
	return func(text string) string {
		text = html.UnescapeString(text)
		text = redact.ReplaceAllString(text, "[redacted]")
		text = strings.Map(func(r rune) rune {
			if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
				return ' '
			}
			return r
		}, text)
		text = diagnosticURL.ReplaceAllStringFunc(text, safeOrigin)
		text = bearerCredential.ReplaceAllString(text, "Bearer [redacted]")
		text = labelledCredential.ReplaceAllString(text, "$1=[redacted]")
		text = redact.ReplaceAllString(text, "[redacted]")
		text = strings.TrimSpace(text)
		if chars := []rune(text); len(chars) > maxDiagnosticChars {
			text = string(chars[:maxDiagnosticChars-1]) + "…"
		}
		return text
	}
}

// safeOrigin deliberately omits the entire path, not just userinfo/query/fragment:
// deployment paths can themselves be credentials. Invalid URLs are not echoed.
func safeOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") ||
		strings.ContainsFunc(u.Host, unicode.IsControl) {
		return "[URL redacted]"
	}
	host := strings.ToLower(u.Hostname())
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host
}

// extractHTTPDiagnostic is called only after the bounded error body completed.
// SDK decoders handle the envelope's error fields (including flat compatible
// shapes); only strictly typed messages and confirmed missing-model facts leave
// this function. Neither raw JSON nor an SDK error is retained in responseState.
func extractHTTPDiagnostic(raw []byte, state *responseState, sanitize func(string) string) {
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return // Never surface HTML/plain-text bodies as diagnostics.
	}
	fields := raw
	if len(envelope.Error) != 0 && string(envelope.Error) != "null" {
		fields = envelope.Error
	}
	var apiErr openai.Error
	if apiErr.UnmarshalJSON(fields) != nil {
		return
	}
	apiErr.StatusCode = state.status
	state.modelMissing = missingModel(&apiErr)
	// Request-validation feedback is actionable (e.g. unsupported stream_options).
	// Authentication and arbitrary server-failure messages are not necessary and
	// frequently echo credentials, so expose no message for those classifications.
	if (state.status == 400 || state.status == 422) && stringField(apiErr.JSON.Message, apiErr.Message, false) {
		state.message = sanitize(apiErr.Message)
	}
}
