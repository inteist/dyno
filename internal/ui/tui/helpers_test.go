package tui

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// testTimeout ends a test whose program hangs, so that it fails instead.
const testTimeout = 10 * time.Second

var errBrokenInput = errors.New("broken input")

// failingReader fails every read, so that the program fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errBrokenInput }

// syncWriter collects the output of a program, whose renderer writes from
// its own goroutine. onWrite runs once, at the first write.
type syncWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	once    sync.Once
	onWrite func()
}

func (w *syncWriter) Write(p []byte) (int, error) {
	if w.onWrite != nil {
		w.once.Do(w.onWrite)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// Len returns the number of bytes written.
func (w *syncWriter) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Len()
}

// String returns the bytes written.
func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// within calls run and fails the test if run does not return in time.
func within(t *testing.T, run func() error) error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- run() }()
	select {
	case err := <-result:
		return err
	case <-time.After(testTimeout):
		t.Fatal("the program did not return")
		return nil
	}
}
