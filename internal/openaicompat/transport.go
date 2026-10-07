package openaicompat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	maxEventBytes  = 1 << 20
	maxErrorBytes  = 64 << 10
	maxModelsBytes = 4 << 20
	maxStreamBytes = 64 << 20
	maxReplyBytes  = 16 << 20
)

type streamGuardKey struct{}
type responseStateKey struct{}

// Only safe facts cross the transport/SDK boundary. Never store a raw error or body.
type responseState struct {
	status       int
	message      string
	modelMissing bool
	err          error
}

func withResponseState(ctx context.Context) (context.Context, *responseState) {
	state := &responseState{}
	return context.WithValue(ctx, responseStateKey{}, state), state
}

// framingGuard observes framing only. The SDK remains the SSE and JSON decoder.
// It does not buffer a whole event or wait for the next frame before releasing
// bytes, and stores only one bounded line plus a tiny terminal-marker candidate.
type framingGuard struct {
	line          []byte
	started       bool
	eventBytes    int
	totalBytes    int
	data          []byte
	dataTooLong   bool
	dataSeen      bool
	events        int
	terminalEvent int
	done          bool
	err           error
}

func (g *framingGuard) observe(p []byte) {
	for _, b := range p {
		if g.err != nil || g.done {
			return
		}
		g.totalBytes++
		g.eventBytes++
		if g.eventBytes > maxEventBytes || g.totalBytes > maxStreamBytes {
			g.err = protocolError("stream size limit exceeded")
			return
		}
		if b != '\n' {
			g.line = append(g.line, b)
			continue
		}
		line := bytes.TrimSuffix(g.line, []byte{'\r'})
		if !g.started {
			line = bytes.TrimPrefix(line, []byte("\xef\xbb\xbf"))
			g.started = true
		}
		if len(line) == 0 {
			if g.dataSeen {
				g.events++
			}
			g.done = !g.dataTooLong && bytes.Equal(g.data, []byte("[DONE]\n"))
			if g.done {
				g.terminalEvent = g.events
			}
			g.eventBytes, g.dataTooLong, g.dataSeen = 0, false, false
			g.data = g.data[:0]
		} else {
			name, value, _ := bytes.Cut(line, []byte{':'})
			value = bytes.TrimPrefix(value, []byte{' '})
			if bytes.Equal(name, []byte("event")) && bytes.Equal(value, []byte("error")) {
				g.err = protocolError("error event")
			}
			if bytes.Equal(name, []byte("data")) {
				g.dataSeen = true
			}
			if bytes.Equal(name, []byte("data")) && !g.dataTooLong {
				if len(g.data)+len(value)+1 > len("[DONE]\n") {
					g.dataTooLong = true
				} else {
					g.data = append(g.data, value...)
					g.data = append(g.data, '\n')
				}
			}
		}
		g.line = g.line[:0]
	}
}

type guardedTransport struct {
	next     http.RoundTripper
	sanitize func(string) string
}

func (t guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Strip SDK platform/runtime metadata at the last boundary, including headers
	// added after RequestOptions run. Do not modify the SDK/caller request.
	req = req.Clone(req.Context())
	for name := range req.Header {
		if strings.HasPrefix(strings.ToLower(name), "x-stainless-") {
			req.Header.Del(name)
		}
	}
	req.Header.Set("User-Agent", "Dyno")
	state, _ := req.Context().Value(responseStateKey{}).(*responseState)
	origin := t.sanitize(safeOrigin(req.URL.String()))
	var tlsFailed atomic.Bool
	trace := &httptrace.ClientTrace{TLSHandshakeDone: func(_ tls.ConnectionState, err error) { tlsFailed.Store(err != nil) }}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	res, err := t.next.RoundTrip(req)
	if err != nil {
		if res != nil && res.Body != nil {
			_ = res.Body.Close()
		}
		if errors.Is(err, context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(err, context.DeadlineExceeded) && !connectionFailure(err) && !tlsFailed.Load() {
			return nil, context.DeadlineExceeded
		}
		return nil, &transportError{origin: origin, cause: transportCause(err), unavailable: connectionFailure(err) || tlsFailed.Load()}
	}
	if state != nil {
		state.status = res.StatusCode
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		_ = res.Body.Close()
		return nil, &HTTPError{StatusCode: res.StatusCode, Kind: "redirect refused"}
	}
	g, _ := req.Context().Value(streamGuardKey{}).(*framingGuard)
	if g != nil && res.StatusCode >= 200 && res.StatusCode < 300 {
		media, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
		if err != nil || media != "text/event-stream" {
			_ = res.Body.Close()
			g.err = protocolError("expected text/event-stream")
			return nil, g.err
		}
		res.Body = &framingBody{ReadCloser: res.Body, guard: g, origin: origin}
	} else {
		limit := maxModelsBytes
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			limit = maxErrorBytes
		}
		body := &boundedBody{ReadCloser: res.Body, remaining: limit, state: state, origin: origin, sanitize: t.sanitize}
		if res.StatusCode >= 400 {
			body.captureError = true
		}
		res.Body = body
	}
	return res, nil
}

type framingBody struct {
	io.ReadCloser
	guard  *framingGuard
	origin string
	reader *bufio.Reader
}

func (b *framingBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.guard.err != nil {
		return 0, b.guard.err
	}
	if b.reader == nil {
		b.reader = bufio.NewReader(b.ReadCloser)
		// SSE permits one leading UTF-8 BOM. The SDK does not strip it; remove
		// only these three framing bytes, without buffering an event or decoding JSON.
		prefix, _ := b.reader.Peek(3)
		if bytes.Equal(prefix, []byte("\xef\xbb\xbf")) {
			_, _ = b.reader.Discard(3)
			b.guard.observe(prefix)
		}
	}
	n, err := b.reader.Read(p)
	b.guard.observe(p[:n])
	if b.guard.err != nil {
		return n, b.guard.err
	}
	// Plain EOF/UnexpectedEOF after HTTP headers is a truncated SSE protocol,
	// not evidence of a network reset. Clean EOF without [DONE] is checked by Chat.
	err = readError(err, b.origin)
	if err != nil && err != io.EOF {
		b.guard.err = err
	}
	return n, err
}

// boundedBody returns an error instead of presenting a truncated body as valid.
// A single look-ahead byte distinguishes a body exactly at the bound from excess.
type boundedBody struct {
	io.ReadCloser
	remaining    int
	closeOnce    sync.Once
	closeErr     error
	state        *responseState
	origin       string
	sanitize     func(string) string
	captureError bool
	errorBody    []byte
}

func (b *boundedBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.ReadCloser.Close() })
	return b.closeErr
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if b.remaining <= 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n != 0 {
			err = fmt.Errorf("%w: %w", ErrProtocol, errResponseSize)
			if b.state != nil {
				b.state.err = err
			}
			b.errorBody = nil
			return 0, err
		}
		return 0, b.observe(nil, err)
	}
	if len(p) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= n
	return n, b.observe(p[:n], err)
}

func (b *boundedBody) observe(p []byte, err error) error {
	if b.captureError {
		b.errorBody = append(b.errorBody, p...)
	}
	err = readError(err, b.origin)
	if b.state != nil {
		if err == io.EOF && b.captureError {
			extractHTTPDiagnostic(b.errorBody, b.state, b.sanitize)
		} else if err != nil && err != io.EOF {
			b.state.err = err // readError discarded arbitrary original causes
		}
	}
	if err != nil {
		b.errorBody = nil
	}
	return err
}

func withStreamGuard(ctx context.Context) (context.Context, *framingGuard) {
	g := &framingGuard{}
	return context.WithValue(ctx, streamGuardKey{}, g), g
}
