package ollama

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"syscall"

	"github.com/ollama/ollama/api"

	"dyno/internal/llm"
)

// mapError maps an error of the api package to the errors of the llm port. It
// returns context errors unchanged.
func mapError(err error) error {
	switch {
	case isContextError(err):
		return err
	case isNotFound(err):
		return fmt.Errorf("%w: %w", llm.ErrModelNotFound, err)
	case isRefused(err):
		return fmt.Errorf("%w: %w", llm.ErrUnavailable, err)
	default:
		return err
	}
}

func isContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// isNotFound reports a 404 response. The api package returns an
// api.StatusError for it, both for streamed and for plain requests.
func isNotFound(err error) bool {
	var status api.StatusError
	return errors.As(err, &status) && status.StatusCode == http.StatusNotFound
}

// isRefused reports a server that does not accept connections.
func isRefused(err error) bool {
	var op *net.OpError
	return errors.Is(err, syscall.ECONNREFUSED) || (errors.As(err, &op) && op.Op == "dial")
}

// errIncomplete is the error of a stream that ended without a done chunk.
func errIncomplete() error {
	return errors.New("the stream ended without a done chunk")
}
