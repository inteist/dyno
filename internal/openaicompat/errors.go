package openaicompat

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/openai/openai-go/v3"

	"dyno/internal/llm"
)

var (
	// ErrDiscoveryUnsupported means no model catalog is available. Supplied IDs
	// can still be validated by inference during warm-up.
	ErrDiscoveryUnsupported = errors.New("model discovery is unsupported; supply model IDs or skip discovery")
	// ErrProtocol denotes incomplete, malformed, or oversized compatible responses.
	ErrProtocol           = errors.New("incompatible response")
	ErrRequestInterrupted = errors.New("request transport interrupted")
	errResponseSize       = errors.New("response size limit exceeded")
)

// HTTPError retains only a status, classification and sanitized, bounded message.
// It never retains SDK errors, bodies, headers, or request URLs.
type HTTPError struct {
	StatusCode int
	Kind       string
	Message    string
	cause      error // only a safe sentinel, never the original error
}

func (e *HTTPError) Error() string {
	text := fmt.Sprintf("HTTP %d (%s)", e.StatusCode, e.Kind)
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

func (e *HTTPError) Unwrap() error { return e.cause }

// transportError describes the underlying cause without retaining an arbitrary
// error graph (which can contain credentials even when Error() is innocuous).
type transportError struct {
	origin, cause string
	unavailable   bool // failure to establish a connection, not a failed exchange
}

func (e *transportError) Error() string {
	return fmt.Sprintf("transport to %s failed: %s", e.origin, e.cause)
}
func (e *transportError) Unwrap() error {
	if e.unavailable {
		return llm.ErrUnavailable
	}
	return ErrRequestInterrupted
}

func protocolError(detail string) error { return fmt.Errorf("%w: %s", ErrProtocol, detail) }

func httpFailure(state *responseState) *HTTPError {
	kind := "server error"
	switch state.status {
	case http.StatusUnauthorized, http.StatusForbidden:
		kind = "authentication/authorization failed"
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		kind = "API route unavailable"
	case http.StatusTooManyRequests:
		kind = "rate limited"
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		kind = "invalid request or context limit"
	}
	return &HTTPError{StatusCode: state.status, Kind: kind, Message: state.message}
}

// mapError never retains raw SDK errors, including their wrapped causes.
func mapError(err error, chat bool, state *responseState) error {
	var transportErr *transportError
	if errors.As(err, &transportErr) {
		return transportErr
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if state.err != nil {
		if state.status >= 400 && errors.Is(state.err, ErrProtocol) {
			e := httpFailure(state)
			e.Message, e.cause = state.err.Error(), ErrProtocol
			return e
		}
		return state.err
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr
	}
	if state.status >= 400 {
		if chat && state.modelMissing {
			return llm.ErrModelNotFound
		}
		return httpFailure(state)
	}
	// All real RoundTrip/read failures are classified at the transport boundary.
	// Remaining errors are SDK JSON/SSE decoding or incompatible response shapes.
	return ErrProtocol
}

func missingModel(e *openai.Error) bool {
	if e.StatusCode != http.StatusBadRequest && e.StatusCode != http.StatusNotFound {
		return false
	}
	if (stringField(e.JSON.Code, e.Code, false) && (e.Code == "model_not_found" || e.Code == "model_not_exists")) ||
		(stringField(e.JSON.Type, e.Type, false) && e.Type == "model_not_found") {
		return true
	}
	if !stringField(e.JSON.Message, e.Message, false) {
		return false
	}
	// A generic route response "Not Found" must not become ErrModelNotFound.
	message := strings.ToLower(e.Message)
	if strings.Contains(message, "model endpoint") || strings.Contains(message, "model route") || strings.HasPrefix(message, "endpoint ") || strings.HasPrefix(message, "route ") || strings.Contains(message, "endpoint /v1/") {
		return false
	}
	return strings.Contains(message, "model") && (strings.Contains(message, "not found") || strings.Contains(message, "does not exist"))
}

// readError distinguishes ordinary body truncation from a real network cause,
// then discards the original graph even for unfamiliar transport implementations.
func readError(err error, origin string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	if isNetworkError(err) {
		return &transportError{origin: origin, cause: transportCause(err)}
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return protocolError("truncated response body")
	}
	if errors.Is(err, ErrProtocol) {
		return ErrProtocol
	}
	return &transportError{origin: origin, cause: transportCause(err)}
}

// Connection failures are fatal; resets/EOF after a connection are exchange
// failures. Never infer server unavailability from a body read.
func connectionFailure(err error) bool {
	var op *net.OpError
	var dns *net.DNSError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	return (errors.As(err, &op) && op.Op == "dial") || errors.As(err, &dns) ||
		errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, http.ErrSchemeMismatch) ||
		errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid) || errors.As(err, &verify) || errors.As(err, &record)
}

func isNetworkError(err error) bool {
	var op *net.OpError
	var dns *net.DNSError
	return errors.As(err, &op) || errors.As(err, &dns) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ETIMEDOUT)
}

// Known causes only: calling an arbitrary error's Error() is not secret-safe.
func transportCause(err error) string {
	switch {
	case errors.Is(err, http.ErrSchemeMismatch):
		return "server gave HTTP response to HTTPS client"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection reset by peer"
	case errors.Is(err, syscall.EPIPE):
		return "broken pipe"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "network timeout"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected EOF"
	case errors.Is(err, io.EOF):
		return "EOF"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// Errno formats an OS-defined code, never an arbitrary error message.
		return errno.Error()
	}
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var verify *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var dns *net.DNSError
	var op *net.OpError
	switch {
	case errors.As(err, &unknown):
		return "TLS certificate signed by unknown authority"
	case errors.As(err, &hostname):
		return "TLS certificate hostname mismatch"
	case errors.As(err, &invalid):
		if invalid.Reason == x509.Expired {
			return "TLS certificate has expired or is not yet valid"
		}
		return "TLS certificate verification failed"
	case errors.As(err, &verify):
		return "TLS certificate verification failed"
	case errors.As(err, &record):
		if string(record.RecordHeader[:]) == "HTTP/" {
			return "server gave HTTP response to HTTPS client"
		}
		return "invalid TLS record; server may be speaking HTTP instead of TLS"
	case errors.As(err, &dns):
		if dns.IsTimeout {
			return "DNS lookup timed out"
		}
		if dns.IsNotFound {
			return "DNS lookup failed: no such host"
		}
		return "DNS lookup failed"
	case errors.As(err, &op):
		if op.Timeout() {
			return "network timeout"
		}
		switch op.Op {
		case "dial", "read", "write":
			return "network " + op.Op + " failed"
		}
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "network timeout"
	}
	return "HTTP transport failure"
}
